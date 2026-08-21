package rpeat

import (
	"fmt"
	"github.com/google/uuid"
	"strings"
	"time"
)

var _ = strings.Split

type depEvt struct {
	JobUUID  uuid.UUID
	Name     string
	JobState JState
}

type JobTrigger map[string]string

// alternate []string{jobuuid_trigger}
// []string{job}
// []string{trigger}  // default to recycle
// OR
// trigger is union of states ??

type Dependency struct {
	// One or more Jobs and associated triggers evaluated
	// at each new depEvt message from Job. The results of each update is
	// used to determine when to trigger Dependency.Action
	//Dependencies map[string]string  // map[Dependency.JobUUID]Dependency.Trigger
	Dependencies JobTrigger // map[Dependency.JobUUID]Dependency.Trigger

	// API action to run on Job with Dependency - these are special cases handled by DependencyClient.Watch()
	Action string // start, stop, kill, hold, restart, etc

	// "all" requires all Dependencies to be true to trigger Action
	// "any" requires at least one Dependency to be true to trigger Action when N is undefined
	// "any" requires at least N Dependency to be true to Trigger Action when N > 0
	// "any" requires at most  N Dependency to be true to Trigger Action when N < 0
	Condition string
	N         int

	// Trigger is used for "all" when only a minimum N is required to satisfy all. Used to disambiguate alternate|triggers
	Trigger string `json:"Trigger,omitempty" xml:"Trigger,omitempty"`

	// Require all jobs to report - otherwise check always returns false
	All bool `json:"All,omitempty" xml:"All,omitempty"`

	// Once a Dependency is true, do not update even when state changes
	UpdateDep bool `json:"UpdateDep,omitempty" xml:"UpdateDep,omitempty"`

	// String parsable into Duration - e.g. 1s, 10m, 1m30s
	Delay string `json:"Delay,omitempty" xml:"Delay,omitempty"`

	// if true, all new triggers from dependencies will queue and job will
	// start immediately after success if new trigger events had been recieved
	QueueJobs bool `json:"QueueJobs,omitempty" xml:"QueueJobs,omitempty"`
}

type DependencyClient struct {
	pool       *DependencyClientPool
	evts       chan *depEvt
	states     map[string]bool   // state of all dependencies must be true to fire
	statenames map[string]string // jstate of each job completed
	completed  map[string]bool   // has job been completed/checked once
	run        bool
	trigger    bool
	contingent bool
	job        *Job
	dependency Dependency
}
type DependencyClientPool struct {
	clients    map[*DependencyClient]bool
	register   chan *DependencyClient
	unregister chan *DependencyClient
	evts       chan *depEvt
}

func NewDependencyClientPool() *DependencyClientPool {
	return &DependencyClientPool{
		clients:    make(map[*DependencyClient]bool),
		register:   make(chan *DependencyClient),
		unregister: make(chan *DependencyClient),
		evts:       make(chan *depEvt),
	}
}
func (pool *DependencyClientPool) AddClient(job *Job, dep Dependency) *DependencyClient {
	ServerLogger.Printf("ADDING CLIENT %s", job.JobUUID)
	var client DependencyClient
	client.states = make(map[string]bool)
	client.completed = make(map[string]bool)
	client.statenames = make(map[string]string)
	for id, _ := range dep.Dependencies {
		client.states[id] = false
		client.completed[id] = false
	}
	if dep.N == 0 && dep.Condition == "any" {
		dep.N = 1
	}
	client.job = job
	client.dependency = dep
	client.evts = pool.evts
	client.pool = pool
	if job.cronStart.isDependent() || job.isController() {
		client.trigger = true
	} else {
		if dep.Dependencies != nil {
			ServerLogger.Printf("CONTINGENCY CLIENT %s", job.JobUUID)
			job.setJobState(JContingent)
			job.setContingent(true)
			client.contingent = true
		}
	}
	pool.register <- &client
	return &client
}
func (pool *DependencyClientPool) RemoveAllClients(job *Job) {
	for c := range pool.clients {
		if c.job.JobUUID == job.JobUUID {
			ServerLogger.Printf("(*DependencyClientPool).Shutdown(*Job) shutting down: %s:%s", job.JobUUID, job.Name)
			delete(pool.clients, c)
			close(c.evts)
		}
	}
}

func (client *DependencyClient) dependenciesSatisfied() bool {
	for _, state := range client.states {
		if state == false {
			return false
		}
	}
	return true
}
func (client *DependencyClient) resetDependencies() {
	ServerLogger.Printf("[resetDependencies] %s:%s", client.job.Name, client.job.JobUUID)
	for id := range client.states {
		client.states[id] = false
		client.completed[id] = false
		client.statenames[id] = ""
	}
	client.run = false
}

func (job *Job) resetContingency() {
	if job.isContingent() {
		ServerLogger.Printf("[resetContingency] Resetting contingency %s:%s", job.Name, job.JobUUID)
		job.setJobState(JContingent)
		job.setContingent(true)
		job.sendUpdate()
	}
}

func (pool *DependencyClientPool) Monitor(evts chan *depEvt) {
	for {
		select {
		case client := <-pool.register:
			pool.clients[client] = true
		case client := <-pool.unregister:
			delete(pool.clients, client)
			close(client.evts)
		case evt := <-evts:
			UpdatesLogger.Printf("%s -> %s", fmt.Sprintf(InfoColor, evt.JobUUID.String()), evt.JobState.ColorizedString())
			for c := range pool.clients {
				c.evts <- evt
			}
		}
		time.Sleep(time.Millisecond * 50) // adding small wait between evts as it seems like they may be getting lost/overwritten?
	}
}

func (client *DependencyClient) CheckDependency(d Dependency, e *depEvt) (isOK, isDepNotOK bool) {
	//client.job.JobsControl.lock.Lock()
	//defer client.job.JobsControl.lock.Lock()

	client.job.lock.Lock()
	defer client.job.lock.Unlock()

	jstate := e.JobState.String()
	if jstate == "manualsuccess" {
		jstate = "success" // manual success is success
	}

	isDependencyOf := false
	deps := d.Dependencies
	for uuid, trigger := range deps { // for each dependency
		triggers := strings.Split(trigger, "|")
		if e.JobUUID.String() == uuid || e.Name == uuid { // is job a dependency

			if stringInSlice(jstate, triggers) { // is job state a trigger
				if jstate == "success" && d.Action == "completed_success" {
					client.completed[uuid] = true
				}
				client.statenames[uuid] = jstate // set statename of dependency e.g. success or failed
				client.states[uuid] = true       // set state to 'true'
				if jstate == "failed" && d.Action == "completed_failed" {
					client.job.JobsControl.lock.Lock()
					client.job.JobsControl.nfailures++
					client.job.JobsControl.lock.Unlock()
				}
				//log.Printf("[[[ %s ]]] Job:%s uuid:%s jstate(trigger):%s Action:%s",fmt.Sprintf(Orange, "processing depEvt"),client.job.Name,uuid,jstate,d.Action)
			} else {
				if stringInSlice(jstate, []string{"failed", "retrying", "held", "stopped", "warning", "warning2", "warning3", "depwarning", "depfailed", "depretry"}) {
					isDepNotOK = true
					client.statenames[uuid] = jstate // set statename of dependency e.g. success or failed
					return
				}
			}
			if !stringInSlice(jstate, triggers) && d.UpdateDep {
				client.states[uuid] = false
			}
			isDependencyOf = true
		}
	}

	if !isDependencyOf {
		return // protects against further processing of non job:trigger pairs
	}
	if d.Condition == "all" {
		N := 0
		isOK = true
		isComplete := true
		for uuid, _ := range deps {
			isOK = isOK && client.states[uuid]
			isComplete = isComplete && client.completed[uuid]
			if client.completed[uuid] {
				N++
			}
		}
	} else if d.Condition == "any" {
		N := 0
		isOK = false
		for uuid, _ := range deps {
			if client.states[uuid] {
				N++
			}
			//log.Printf("CheckDependency [%s %s] N:%d d.N:%d", client.job.Name, d.Action, N, d.N)
			if N >= d.N {
				isOK = true
				break
			}
		}
	}

	if client.job.IsRunning && stringInSlice(d.Action, []string{"start", "cronstart"}) {
		//log.Printf(ErrorColor, fmt.Sprintf("Current Job [%s] is Running - DO NOT EXECUTE %s", client.job.JobUUID, d.Action)) // FIXME: this should depend on Concurrency in StartRule, need to address logging and GUI
		if !d.QueueJobs {
			client.resetDependencies() // FIXME: should _not_ reset "running" jobs to false
		}
		isOK = false
	}
	//client.job.JobsControl.lock.Lock()
	if d.Action == "start" && client.job.JobsControl.nfailures > d.N {
		// DO NOT START
		client.resetDependencies()
		client.job.JobsControl.lock.Lock()
		client.job.JobsControl.nfailures = 0
		client.job.JobsControl.lock.Unlock()
		isOK = false
	}
	//client.job.JobsControl.lock.Unlock()
	return
}

func (d DependencyClient) delay() {
	if d.dependency.Delay != "" {
		delay, err := time.ParseDuration(d.dependency.Delay)
		if err != nil {
			// FIXME: can't be fatal and must be caught at parse
			ServerLogger.Fatal("error parsing dependency duration string:", err)
		}
		//ServerLogger.Println("sleeping...")
		time.Sleep(delay)
		//log.Println("done sleeping.")
	}
}

// DependencyClient.Watch is run for each Job (Client)
// to monitor upstream dependencies, and trigger job's
// timer to start job.
// func (client *DependencyClient) Watch(wg *sync.WaitGroup) {
func (client *DependencyClient) watch() {
	//wg.Done()
	for {
		select {
		case e := <-client.evts:
			dep := client.dependency
			//ServerLogger.Printf("Checking Dependency:%s Action:%s for %s", e.Name, dep.Action, client.job.JobUUID)
			ok, depNotOk := client.CheckDependency(dep, e)
			if ok {
				ServerLogger.Printf("Dependency Triggered:%s Action:%s for %s (depNotOk:%t)", e.Name, dep.Action, client.job.JobUUID, depNotOk)
				client.resetDependencies() // FIXME: should _not_ reset "running" jobs to false
				if client.trigger {
					switch dep.Action {
					case "start":
						if !client.run {
							ServerLogger.Printf("[[[ %s ]]] START TRIGGERED Job:%s uuid:%s jstate(trigger):%s Action:%s", fmt.Sprintf(Orange, "processing depEvt"), client.job.Name, e.JobUUID.String(), e.JobState.String(), dep.Action)
							client.delay()
							client.job.resetTimer(0)
							client.run = true
						}
					case "cronstart":
						if !client.run {
							ServerLogger.Printf("[[[ %s ]]] CRON START TRIGGERED Job:%s uuid:%s jstate(trigger):%s Action:%s", fmt.Sprintf(Orange, "processing depEvt"), client.job.Name, e.JobUUID.String(), e.JobState.String(), dep.Action)
							client.job.setHold(false)
							client.job.setJobState(JReady)
							client.job.resetContingency()
							//client.delay()
							client.job.sendUpdate()
						}
					case "stop":
						if client.job.IsRunning {
							ServerLogger.Printf("[[[ %s ]]] STOP TRIGGERED Job:%s uuid:%s jstate(trigger):%s Action:%s", fmt.Sprintf(Orange, "processing depEvt"), client.job.Name, e.JobUUID.String(), e.JobState.String(), dep.Action)
							stopJob(client.job, JStopped)
							//client.resetDependencies()
						}
						client.run = false
					case "restart":
						client.delay()
						stopJob(client.job, JEnd)
						time.Sleep(time.Second * 1)
						client.job.resetTimer(0)
						client.run = true
					case "ready": // reset all child jobs
						//log.Printf("<<< RESET >>> %s", client.job.Name)
						client.run = false
						stopJob(client.job, JStopped) // kill job
						client.job.setHold(false)
						client.job.setJobState(JReady)
						client.resetDependencies()
						//client.delay()
						client.job.sendUpdate()
					//case "reset":
					//    client.job.resetContingency()
					case "completed_failed":
						ServerLogger.Printf("[[[ %s ]]] 👎  COMPLETED FAILED Job:%s uuid:%s jstate(trigger):%s Action:%s nfailures:%d", fmt.Sprintf(Orange, "processingdepEvt"), client.job.Name, e.JobUUID.String(), e.JobState.String(), dep.Action, client.job.JobsControl.nfailures)
						client.resetDependencies()
						var c Ctl
						c.killed = false
						c.code = JFailed
						client.job.Ctl <- &c
					case "completed_stopped":
						var c Ctl
						c.killed = false
						c.code = JStopped
						client.job.Ctl <- &c
					case "completed_success": // used for parent of Job.Jobs
						ServerLogger.Printf("[[[ %s ]]] 👍  COMPLETED SUCCESS Job:%s uuid:%s jstate(trigger):%s Action:%s", fmt.Sprintf(Orange, "processig depEvt"), client.job.Name, e.JobUUID.String(), e.JobState.String(), dep.Action)
						var c Ctl
						c.killed = false
						c.code = JSuccess
						client.job.Ctl <- &c
					default:
						ServerLogger.Printf("unsupported Dependency.Action: %s", dep.Action)
					}
					if dep.QueueJobs {
						time.Sleep(time.Second)
					}
				} else {
					if client.contingent {
						client.job.setJobState(JReady)
						client.job.setContingent(false)
						if client.job.cronStart.every > 0 {
							client.job.resetTimer(client.job.cronStart.every)
						}
						client.job.sendUpdate()
					}
				}
			}
			if depNotOk { // Dependency Warning - dependency is in non-triggered state that is temporarily stuck/failed
				switch e.JobState {
				case JRetrying:
					client.job.setHold(false)
					client.job.setJobState(JDepRetry)
				case JFailed:
					client.job.setHold(false)
					client.job.setJobState(JDepFailed)
				default:
					client.job.setHold(false)
					client.job.setJobState(JDepWarning)
				}
				//if client.contingent {
				//    client.job.setContingent(true)
				//	client.job.setHold(true)
				//}
				//client.job.setHold(false)
				//client.job.setJobState(JDepWarning)  // FIXME: this should be JDep[Warning|Failed|Retry] for more granular control of job controls
				client.resetDependencies()
				client.job.sendUpdate()
			}
		}
	}
}

type DependencyGraphs struct {
	Name         string            `json:"Name"`
	JobUUID      string            `json:"JobUUID"`
	Dependencies []DependencyGraph `json:"Dependencies"`
}
type DependencyGraph struct {
	Action       string                      `json:"action"`
	Condition    string                      `json:"condition"`
	Delay        string                      `json:"delay"`
	TriggerUUIDs JobTrigger                  `json:"triggerUUIDs"`
	TriggerNames JobTrigger                  `json:"triggerNames"`
	Triggers     map[string]DependencyGraphs `json:"triggers"`
}

func (job *Job) getDependencyGraph(sd *ServerData) DependencyGraphs {
	jobmap := sd.jobs
	var depGraph []DependencyGraph
	if job.Dependency != nil {
		depGraph = make([]DependencyGraph, len(job.Dependency))
		for i, dep := range job.Dependency {
			depGraph[i].Triggers = make(map[string]DependencyGraphs)
			depGraph[i].Action = dep.Action
			depGraph[i].Condition = dep.Condition
			depGraph[i].Delay = dep.Delay
			depGraph[i].TriggerUUIDs = make(JobTrigger)
			depGraph[i].TriggerNames = make(JobTrigger)
			for trigger, state := range dep.Dependencies {
				if j, ok := jobmap[sd.jobNameUUID[trigger].String()]; ok {
					if j.JobUUID != job.JobUUID {
						depGraph[i].Triggers[j.JobUUID.String()] = j.getDependencyGraph(sd)
					}
					depGraph[i].TriggerUUIDs[j.JobUUID.String()] = state
					depGraph[i].TriggerNames[j.Name] = state
				} else {
					// dependency not found, but keep definition for reference in validation
					depGraph[i].TriggerUUIDs[trigger] = state
					depGraph[i].TriggerNames[trigger] = state
				}
			}
		}
	}
	return DependencyGraphs{Name: job.Name, JobUUID: job.JobUUID.String(), Dependencies: depGraph}
}

// return string representation for validation output
func (g DependencyGraphs) Print_() {
	for _, dep := range g.Dependencies {
		fmt.Printf("      Dependencies:\n        \"%s\" (requires %s):\n", dep.Action, dep.Condition)
		for trigger, state := range dep.TriggerNames {
			fmt.Printf("          - %s [%s]\n", trigger, state)
		}
	}
}

func (g DependencyGraphs) Print(top bool, lpad int) {
	if top {
		//fmt.Printf("      Dependencies:\n          %s\n", g.Name)
		//fmt.Println("\n    Dependencies:")
		fmt.Println()
	}
	lpad = 4 + lpad
	for _, dep := range g.Dependencies {
		fmt.Printf("%s\033[1;38;5;8mAction:\033[0m %s | \033[1;38;5;8mCondition:\033[0m %s | Delay: %s\n", strings.Repeat(" ", lpad), dep.Action, dep.Condition, dep.Delay)
		for uuid, t := range dep.Triggers {
			fmt.Printf("  %s  \033[1;38;5;202m\u2196\033[0m \033[1m%s\033[0m \033[38;5;7mtrigger:\033[0m\033[38;5;39m%s\033[0m\n", strings.Repeat(" ", lpad), t.Name, dep.TriggerUUIDs[uuid])
			if len(t.Dependencies) > 0 {
				t.Print(false, lpad)
			}
		}
	}
}

// draw dependencies in HTML
func (g DependencyGraphs) HTML(top bool, name string, lpad int, html string) string {
	// ref: gui.go:L203
	if top {
		html = "<br/>"
	}
	lpad = 4 + lpad
	for _, dep := range g.Dependencies {
		html = html + fmt.Sprintf("<div style='color: orange; padding-top:0.6ch; min-width: 300px; text-align: left;'>%s&nbsp;&nbsp;Action: <span stype='color:#777'>%s</span> | Condition: <span style='color:#777'><i>%s</i></span> | <span style='color: black'>Delay: %s</span></div>", strings.Repeat("&nbsp;", lpad), dep.Action, dep.Condition, dep.Delay)
		for uuid, t := range dep.Triggers {
			html = html + fmt.Sprintf("<div>%s&nbsp;&nbsp;&nbsp;<b style='padding-left: 1ch; color:#555;'>&nwarr;</b>&nbsp;&nbsp;&nbsp;<b><span data-dep-jobuuid='%s'>%s</b> <span style='padding-left:2ch;color:#BBB;'>trigger:<span style='color:#999;'><b>%s</b></span></div>", strings.Repeat("&nbsp;", lpad), uuid, t.Name, strings.Join(strings.Split(dep.TriggerUUIDs[uuid], "|"), "&nbsp;<b color=lightgrey>|</b>&nbsp;"))
			if len(t.Dependencies) > 0 {
				html = t.HTML(false, name, lpad, html)
			}
		}
	}
	return html + "</br>"
}

/*
function getDependencies(d, name="job", lpad=0) {
  let job = d[name];
  console.log();
  if (job.Dependencies === null ) return null;
    if(name=="job") {
      console.log(job.Name+" ("+job.JobUUID+")");
    }
    lpad = 4+lpad;
    job.Dependencies.forEach( (x) => {
    console.log(" ".repeat(lpad)+"  "+x.action+" ("+x.condition+")");
    for( const t of Object.values(x.triggers) ) {
      console.log(" ".repeat(lpad)+"  \u2196 "+t.Name+" ("+t.JobUUID+")");
      if (t.Dependencies !== null) {
        getDependencies(x.triggers, t.JobUUID, lpad=1*lpad);
      }
    }
  })
}
*/
