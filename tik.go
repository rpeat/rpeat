package rpeat

import (
	"fmt"
	"github.com/google/uuid"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Ctl struct {
	killed bool
	code   JState
}

type Signal struct {
	Sig int
}

func RegisterJob(job *Job, updates chan *JobUpdate, depEvt chan *depEvt, stop <-chan bool, wg *sync.WaitGroup) {

	if len(job.Logging.Logs) > 0 {
		n := job.Logging.Logs[0] // next log(s) in time
		job.Logging.l.Reset(n.PrevStop.Add(job.Logging.purge).Sub(time.Now()))
	}

	d, next := NextCronStart(job.cronStartArray)
	job.t = time.NewTimer(d)

	job.setNextStart(next)
	job.updates = updates
	job.state = depEvt

	maxd := job.getMaxDuration()

	te := time.NewTimer(math.MaxInt64)
	tr := time.NewTimer(math.MaxInt64)

	// channel to return/free endAtTime go routines
	mend := make(chan bool)
	eend := make(chan bool)
	rend := make(chan bool)

	pid := make(chan int)
	job.status = make(chan int)
	ctl := make(chan *Ctl, 3)
	job.Ctl = make(chan *Ctl, 3)

	var thispid, s int

	go func(job *Job) {
		for {
			select {
			case <-job.Logging.l.C:
				e := job.Logging.Logs[0] // log(s) to be removed
				job.Logging.Logs = job.Logging.Logs[1:]
				lfiles := e.LogFiles
				ServerLogger.Printf("LOG CLEANUP %s:%s stderr:%s", job.JobUUID, job.Name, strings.Join(lfiles, ","))
				for _, lf := range lfiles {
					if err := os.Remove(lf); err != nil {
						ServerLogger.Printf("Error removing %s: %s", lf, err)
					}
				}
				if len(job.Logging.Logs) > 0 {
					n := job.Logging.Logs[0] // next log(s) in time
					removeAt := n.PrevStop.Add(job.Logging.purge)
					ServerLogger.Printf("Setting logs removal on %s:%s for %s", job.JobUUID, job.RunUUID, removeAt.Round(time.Second))
					job.Logging.l.Reset(removeAt.Sub(time.Now()))
					//job.Logging.l.Reset(n.Value.(JobLog).prevStop.Add(time.Duration(time.Second * 70)).Sub(time.Now()))
				}
				job.SaveSnapshot(true)
			}
		}
	}(job)

	retry := 0
	wg.Done()
	for {
		job.setRetryAttempt(retry)
		if job.WaitForTrigger(stop) != nil {
			return
		}
		// job has been triggered  |
		//                         |
		//                         V
		ServerLogger.Printf("Is 'unscheduled' Trigger? %t", job.Unscheduled)

		job.runlock.Lock()
		job.t.Stop()

		d, next = NextCronStart(job.cronStartArray)
		job.setNextStart(next)
		ServerLogger.Printf("Next job %s <%s> scheduled for %s [%d] (starts in %s)\n", job.Name, job.JobUUID, next, next.Unix(), d.Round(time.Second))

		if job.Updating {
			job.resetTimer(d)
			job.Updating = false
			job.sendUpdate()
			job.runlock.Unlock()
			continue
		}

		if job.Hold {
			ServerLogger.Printf("Job Trigger Ignored Job on Hold")
			job.resetTimer(d)
			job.setJobState(JMissedWarning)
			job.sendUpdate() // keep jobs on Hold updating next scheduled start
			job.runlock.Unlock()
			continue
		}

		maxduration := time.NewTimer(maxd)
		if job.Retry > 0 {
			go runTik(job, pid, true)
		} else {
			go runTik(job, pid, false)
		}
		if job.startRule.Concurrent || job.cronStart.IsEvery() {
			job.resetTimer(d)
			//ServerLogger.Printf("[Concurrent] next job %s<%s> scheduled for %s [%d] (starts in %s)\n", job.Name, job.JobUUID, next, next.Unix(), d)
		}

		// main block to wait for job to complete
		if !job.Hold {
			thispid = <-pid

			// reset all Cron timers
			if job.MaxDuration != "" && job.startRule.Concurrent == false {
				go endAtTime(maxduration, job, ctl, "maxduration", mend) //, false)
			}
			if job.CronEnd != nil {
				e, _ := NextCronStart(job.cronEndArray)
				te.Reset(e)
				go endAtTime(te, job, ctl, "cronend", eend)
			}
			if job.CronRestart != nil {
				r, _ := getNextStart(job.cronRestart)
				tr.Reset(r)
				ServerLogger.Printf("Running job %s scheduled to RESTART %s [%d] (ends in %s)\n", job.Name, next, next.Unix(), r.Round(time.Second))
				go func() {
					if endAtTime(tr, job, ctl, "cronrestart", rend) {
						time.Sleep(time.Second * time.Duration(1))
						job.setHold(false)
						job.resetTimer(0)
					}
				}()
			}

			s = <-job.status // blocks until runTik completes
		}

		// stop all go routines watching for end/restart/maxduration triggers
		isStopped := maxduration.Stop()
		if job.CronEnd != nil {
			isStopped = te.Stop()
			ServerLogger.Printf("CronEnd (te):%t", isStopped)
			if isStopped {
				eend <- true
			}
		}
		if job.CronRestart != nil {
			isStopped = tr.Stop()
			ServerLogger.Printf("CronRestart (tr):%t", isStopped)
			if isStopped {
				rend <- true
			}
		}

		if !job.startRule.Concurrent && !job.cronStart.IsEvery() {
			d, next = NextCronStart(job.cronStartArray)
			job.resetTimer(d)
			job.setNextStart(next)
			job.sendUpdate()
			ServerLogger.Printf("[Not Concurrent] next job %s scheduled for %s [%d] (starts in %s)\n", job.Name, next, next.Unix(), d)
		}

		if job.Hold {
			//log.Printf("held job continue - back to top of loop")
			job.runlock.Unlock()
			continue
		}

		// retry attempts
		for {
			// during retry we need to suspend or modify original timer to as not to have concurrent jobs
			job.setRetryAttempt(retry)

			if s == 0 || job.Hold {
				break
			}
			if s != 0 && job.Retry > retry {
				job.t.Stop() //stop current job while we retry

				if !job.Restarting {
					job.setJobState(JRetryWait)
					retry = retry + 1
					job.setRetryAttempt(retry)
					job.resetTimer(job.getRetryWait(retry))
					job.setJobState(JRetryWait)
					job.sendUpdate()
					if job.WaitForTrigger(stop) != nil {
						return // external Ctrl-C to shutdown server
					}
				} else {
					ServerLogger.Printf("restarting job (%t) %s", job.Restarting, job.Name)
					retry = 0
					job.setRetryAttempt(retry)
					job.setJobState(JRunning)
					job.sendUpdate()
				}
				maxduration.Reset(maxd)
				go runTik(job, pid, !job.Restarting)
				thispid = <-pid
				if job.MaxDuration != "" {
					go endAtTime(maxduration, job, ctl, "maxduration(retry)", mend)
				}
				s = <-job.status
				if s == 0 {
					d, next = NextCronStart(job.cronStartArray)
					//if !job.cronStart.IsEvery() {
					job.resetTimer(d)
					//}
					ServerLogger.Printf("[Retry Success] next job %s scheduled for %s [%d] (starts in %s)\n", job.Name, next, next.Unix(), d)
					retry = 0
					job.setRetryAttempt(retry)
					job.setNextStart(next)
					if job.JobState == JRunning {
						if job.Unscheduled {
							job.setJobState(JManualSuccess)
						} else {
							job.setJobState(JSuccess)
						}
					}
					job.sendUpdate()
				}
			}
			/*
			   if job.startRule.Concurrent {
			       if !job.t.Stop() {
			          <-job.t.C
			       }
			       job.t.Reset(d)  // I think this isn't correct -  FIXME
			   } else {
			     break  // no concurrency shouldn't result in first attempt ending retries FIXME
			   }
			*/

			if job.Retry == retry { // reset timer
				job.t.Stop() //stop current job while we retry
				//log.Printf("job %s has failed %d times. Terminating with permanent failure\n", job.Name, job.Retry)
				proc, err := os.FindProcess(thispid)
				if err != nil || thispid == 0 {
					ServerLogger.Printf("error: %s", err)
				} else {
					ServerLogger.Printf("proc(%v).Kill()", proc)
					proc.Kill()
				}
				if !job.cronStart.isDependent() {
					job.setHold(true)
				}
				job.setJobState(JFailed)
				d, next = NextCronStart(job.cronStartArray)
				job.resetTimer(d)
				retry = 0
				job.setRetryAttempt(retry)
				job.setNextStart(next)
				job.sendUpdate()
				break
			}

		}
		job.runlock.Unlock()
	}
}

func runTik(job *Job, pid chan int, retry bool) {
	var errcode int
	var evt *Ctl

	if job.Hold {
		ServerLogger.Printf("Job [%s] on hold - not run", job.Name)
		pid <- 0
		job.status <- 0
		return
	}
	if !job.currentHost() {
		ServerLogger.Printf("Job [%s] not scheduled to run on job.Host:%s", job.Name, job.Host)
		return
	}

	// Controller Job
	if job.isController() {
		job.prevStart = time.Now()
		job.StartedUNIX = job.prevStart.Unix()
		job.Started = job.prevStart.In(job._location).Format("2006-01-02 15:04:05")
		job.JobsControl.lock.Lock()
		job.JobsControl.nfailures = 0
		job.JobsControl.lock.Unlock()
		job.setJobState(JRunning)
		job.sendUpdate()
		select {
		case evt = <-job.Ctl:
			//log.Printf("Controller finish message for [%s]", job.JobUUID)
			job.prevStop = time.Now()
			job.PrevStop = job.prevStop.In(job._location).Format("2006-01-02 15:04:05")
			job.PrevStart = job.Started
			job.elapsed = job.prevStop.Sub(job.prevStart).Round(time.Second)
			job.Elapsed = dhms(job.elapsed)
			job.ElapsedUNIX = elapsedToInt(job.elapsed)
			job.setJobState(evt.code)
			if evt.code == JStopped || evt.code == JFailed {
				job.setHold(true)
				//for _, depjob := range job.Jobs {
				//    //log.Printf("killing dependent job [%s] if running.", depjob.JobUUID)
				//}
			}
			job.sendUpdate()
			pid <- 0
			job.status <- 0
			return
		}
		//log.Printf("finished controller block at top of runTick")
	}

	job.RunUUID = uuid.New()

	if job.Cmd != nil {

		c, err := evaluatedCmd(job, false, "")
		if err != nil {
			ServerLogger.Printf(err.Error())
		}

		// Kill child processes under *nix
		// https://medium.com/@felixge/killing-a-child-process-and-all-of-its-children-in-go-54079af94773
		// https://varunksaini.com/posts/kiling-processes-in-go/
		//
		//c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		c.SysProcAttr = syscallSysProcAttr()

		// SetUID and GID
		// https://stackoverflow.com/questions/21705950/running-external-commands-through-os-exec-under-another-user
		//
		// c.SysProcAttr.Credential = &syscall.Credential{Uid: uid, Gid: gid}

		jobRunDir := filepath.Join(job.TmpDir, fmt.Sprintf("%s", job.JobUUID))

		var ferr error
		var stdout []io.Writer
		var stdoutfile *os.File

		flags := os.O_RDWR | os.O_CREATE
		if job.Logging.Append {
			flags = flags | os.O_APPEND
		}

		// if Logging.Std*File may change to slices to allow for tee style behavior
		if job.Logging.StdoutFile != "" {
			stdoutfile, ferr = os.OpenFile(job.ExpandEnv([]string{job.Logging.StdoutFile}, "")[0], flags, os.FileMode(0660))
			if ferr != nil {
				ServerLogger.Fatal(ferr.Error())
			}
		} else {
			_ = os.Mkdir(jobRunDir, os.FileMode(0770))
			stdoutfile, ferr = os.Create(filepath.Join(jobRunDir, fmt.Sprintf("%s.stdout", job.RunUUID)))
			if ferr != nil {
				ServerLogger.Fatal(ferr.Error())
			}
		}
		job.Logging.stdoutFile = stdoutfile.Name()
		job.StdoutFile = []string{job.Logging.stdoutFile}

		if job.stdout {
			stdout = append(stdout, os.Stdout) // will only work if running single job - not for scheduled!!!!
		}
		stdout = append(stdout, stdoutfile)
		c.Stdout = io.MultiWriter(stdout...)

		var stderr []io.Writer
		var stderrfile *os.File

		if job.Logging.StderrFile != "" {
			stderrfile, ferr = os.OpenFile(job.ExpandEnv([]string{job.Logging.StderrFile}, "")[0], flags, os.FileMode(0660))
			if ferr != nil {
				ServerLogger.Fatal(ferr.Error())
			}
		} else {
			_ = os.Mkdir(jobRunDir, os.FileMode(0770))
			stderrfile, ferr = os.Create(filepath.Join(jobRunDir, fmt.Sprintf("%s.stderr", job.RunUUID)))
			if ferr != nil {
				ServerLogger.Fatal(ferr.Error())
			}
		}
		job.Logging.stderrFile = stderrfile.Name()
		job.StderrFile = []string{job.Logging.stderrFile}

		if job.stderr {
			stderr = append(stderr, os.Stderr) // will only work if running single job - not for scheduled!!!!
		}
		stderr = append(stderr, stderrfile)
		c.Stderr = io.MultiWriter(stderr...)

		err = c.Start()
		if err != nil {
			ServerLogger.Printf("[runTik] %s failed to start with error ( %s )", job.Name, err)
			job.Lock()
			job.pid = 0
			job.Pid = 0
			pid <- 0
			job.IsRunning = false
			job.Failed = true

			job.prevStart = time.Now()
			job.StartedUNIX = job.prevStart.Unix()
			job.Started = job.prevStart.In(job._location).Format("2006-01-02 15:04:05")
			job.prevStop = time.Now()
			job.PrevStop = job.prevStop.In(job._location).Format("2006-01-02 15:04:05")
			job.PrevStopUNIX = job.prevStop.Unix()
			job.PrevStart = job.Started
			job.elapsed = job.prevStop.Sub(job.prevStart).Round(time.Second)
			job.Elapsed = dhms(job.elapsed)
			job.ElapsedUNIX = elapsedToInt(job.elapsed)
			job.Unscheduled = true
			_, err := c.Stderr.Write([]byte("[ rpeat ] Unable to create process (possibly missing shell e.g. /bin/sh -c ): " + err.Error()))
			retry = false
			if err != nil {
				panic(err)
			}

			job.status <- -1
			job.lock.Unlock()
			job.setJobState(JFailed)
			job.sendUpdate()
			return
		}
		job.Lock()
		job.IsRunning = true
		job.Failed = false
		// https://stackoverflow.com/questions/11886531/terminating-a-process-started-with-os-exec-in-golang
		job.proc = c.Process
		job.procState = c.ProcessState
		job.Pid = c.Process.Pid
		job.pid = job.Pid
		pid <- job.pid // alert caller to process id

		job.prevStart = time.Now()
		job.StartedUNIX = job.prevStart.Unix()
		job.Started = job.prevStart.In(job._location).Format("2006-01-02 15:04:05")

		job.lock.Unlock()
		job.setJobState(JRunning)
		job.sendUpdate()

		err = c.Wait() // blocks until job ends, possibly also sending <-job.Ctl if external trigger

		errcode = 0
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
					ServerLogger.Printf("Unexpected exit for %s with status:%d", job.JobUUID, status.ExitStatus())
					job.Unscheduled = true
					errcode = status.ExitStatus()
				}
			}
		}
		job.Lock()
		job.IsRunning = false
		job.prevStop = time.Now()
		job.PrevStop = job.prevStop.In(job._location).Format("2006-01-02 15:04:05")
		job.PrevStopUNIX = job.prevStop.Unix()
		job.PrevStart = job.Started
		job.elapsed = job.prevStop.Sub(job.prevStart).Round(time.Second)
		job.Elapsed = dhms(job.elapsed)
		job.ElapsedUNIX = elapsedToInt(job.elapsed)

		// if job.elapsed < job.MinDuration {
		//    job.SetJobState(KWarning)
		// }

		job.Pid = 0
		job.pid = job.Pid
		job.lock.Unlock()

	} else {
		// job.Jobs is defined? if so build new rpeat or start first job
		job.setJobState(JRunning)
		job.sendUpdate()
		// wait on Dependencies ALL or ANY
	}

	if job.Logging.purge < math.MaxInt64 {
		jlog := JobLog{PrevStop: job.prevStop, LogFiles: []string{job.Logging.stderrFile, job.Logging.stdoutFile}}
		//job.Logging.Logs.PushBack(jlog) // use this for adding Duration to for removal
		job.Logging.Logs = append(job.Logging.Logs, jlog)
		if len(job.Logging.Logs) == 1 {
			// FIXME need to use job.Logging.purge as duration
			ServerLogger.Printf("Setting logs removal on %s:%s for %s", job.JobUUID, job.RunUUID, time.Now().Add(job.Logging.purge).Round(time.Second))
			job.Logging.l.Reset(job.Logging.purge)
			job.SaveSnapshot(true)
		}
	}

	select {
	//case evt = <-ctl:
	case evt = <-job.Ctl:
		//job.ExitCode = int(evt.code)
		job.ExitCode = errcode
		job.setJobState(JState(evt.code))
		if JState(evt.code) == JRestart {
			ServerLogger.Printf("Restart triggered on %s", job.JobUUID)
			job.status <- 0
			job.setJobState(JEnd)
			job.sendUpdate()
			time.Sleep(time.Second * 5)
			job.resetTimer(0)
			ServerLogger.Printf("Restart timer reset on %s", job.JobUUID)
			return
		}
		job.sendUpdate()
		job.status <- 0
		//job.resetContingency()
		return
	default:
		job.ExitCode = errcode
		evt = &Ctl{killed: false, code: JState(errcode)}
		if errcode == 0 {
			if job.Unscheduled {
				job.setJobState(JManualSuccess)
			} else {
				job.setJobState(JSuccess)
			}
			job.status <- 0
		} else {
			if retry && job.RetryAttempt < job.Retry {
				job.setJobState(JRetryFailed)
			} else {
				job.setJobState(JFailed)
			}
			job.status <- -1
		}
	}
	job.ExitCode = errcode
	//job.sendUpdate()
	//job.resetContingency()
	job.Unscheduled = false

	//fmt.Println("SysUsage", c.ProcessState.SysUsage())
}
func shutdownJob(job *Job, jstate JState) {
	ServerLogger.Printf("[shutdownJob] triggered for %s (%s => %s)", job.JobUUID, job.JobState, jstate)

	pid := job.pid
	if pid == 0 && job.cronStart.isDependent() {
		//ServerLogger.Printf("[shutdownJob] %s job has already exited", job.JobUUID)
		return
	}

	c, _ := evaluatedCmd(job, true, "")
	c.SysProcAttr = syscallSysProcAttr()
	err := c.Run()
	if err != nil {
		ServerLogger.Println("shutdown failure:", err)
	}

	/* FIXME: do we need this? Likely? */
	var ctl Ctl
	ctl.killed = true
	ctl.code = jstate
	job.Ctl <- &ctl
	ServerLogger.Printf("[shutdownJob] %s %s Ctl sent!", job.JobUUID, jstate)
	ServerLogger.Printf("[shutdownJob] completed for %s", job.JobUUID)

	if jstate == JStopped {
		job.setHold(true)
	}
	time.Sleep(time.Second * 1)
	job.setJobState(JHold)
	job.sendUpdate()
}

func resumeJob(job *Job) {
	if job.isController() {

	}
}

func stopJob(job *Job, jstate JState) {

	pid := job.getPid()
	ServerLogger.Printf("[stopJob] triggered for %s (%d, %s)", job.JobUUID, pid, job.JobState)
	if pid == 0 && job.cronStart.isDependent() { // FIXME: need to disable stops if job isn't running
		ServerLogger.Printf("[stopJob] %s job has already exited", job.JobUUID)
		return
	}
	if !job.cronStart.isDependent() {
		job.setHold(true)
	}
	if !job.isController() && pid != 0 { // should only signal stopped job if running

		var c Ctl
		c.killed = true
		c.code = jstate
		job.Ctl <- &c

		_, err := os.FindProcess(pid)
		if err != nil {
			//log.Printf("[stopJob] error: %s", err.Error())
		}

	}
	if job.JobState == JRetryWait {
		job.setHold(true)
		job.setJobState(JState(jstate))
		job.setRetryAttempt(0)
		job.resetTimer(0)
		job.sendUpdate()
	}
	job.Lock()
	job.pid = 0
	job.Pid = 0
	job.prevStop = time.Now()
	job.PrevStop = job.prevStop.In(job._location).Format("2006-01-02 15:04:05")
	job.PrevStopUNIX = job.prevStop.Unix()
	job.PrevStart = job.prevStart.In(job._location).Format("2006-01-02 15:04:05") // only update after job completes to maintain "previous" and not "current"
	job.elapsed = job.prevStop.Sub(job.prevStart).Round(time.Second)
	job.Elapsed = dhms(job.elapsed)
	job.ElapsedUNIX = elapsedToInt(job.elapsed)
	job.Unlock()

	sig := os.Kill
	switch job.ShutdownSig {
	case "SIGINT", "Interrupt":
		sig = os.Interrupt
	case "SIGKILL", "Kill":
		sig = os.Kill
	}

	if pid != 0 {
		//log.Printf("[stopJob] sending SIGKILL to pgid:%d", pid)
		pgid, err := syscallGetpgid(pid)
		if err != nil {
			ServerLogger.Printf("[stopJob] error: %s", err.Error())
		}
		//if err = syscallKill(-pgid, sig.(syscall.Signal)); err != nil {
		if err = syscallKill(-pgid, sig); err != nil {
			ServerLogger.Printf("[stopJob] error: %s", err.Error())
		}
	}
	if !job.cronStart.isDependent() {
		job.setHold(true)
	}
	if jstate == JEnd {
		job.setHold(false)
	}
	//job.sendUpdate()
	//log.Printf("[stopJob] killing process %d (this may take a moment)", pid)
}

func endAtTime(t *time.Timer, job *Job, ctl chan *Ctl, caller string, e chan bool) bool {
	//log.Printf("[endAtTime] go routine (%s) for %s %s:%s", caller, job.Name, job.JobUUID, job.RunUUID)
	select {
	case <-e:
		ServerLogger.Printf("[endAtTime] cancelling %s for %s:%s:%s", caller, job.JobUUID, job.RunUUID, job.Name)
		return false
	case <-t.C:
		//log.Printf("[endAtTime] %s timer triggered for %s:%s:%s !", caller, job.JobUUID, job.RunUUID, job.Name)
	}
	t.Stop()

	if job.ShutdownCmd != "" {
		shutdownJob(job, JEnd)
		return true
	}
	// set type of stop
	pid := job.getPid()
	if pid == 0 {
		//log.Printf("[stopJob] %s job has already exited", job.JobUUID)
		return true
	}

	var c Ctl
	c.killed = true
	c.code = JEnd
	job.Ctl <- &c
	//log.Println("endAtTime Ctl sent!")

	job.Lock()
	job.prevStop = time.Now()
	job.PrevStop = job.prevStop.In(job._location).Format("2006-01-02 15:04:05")
	job.PrevStopUNIX = job.prevStop.Unix()
	job.PrevStart = job.prevStart.In(job._location).Format("2006-01-02 15:04:05") // only update after job completes to maintain "previous" and not "current"
	job.elapsed = job.prevStop.Sub(job.prevStart).Round(time.Second)
	job.Elapsed = dhms(job.elapsed)
	job.ElapsedUNIX = elapsedToInt(job.elapsed)

	sig := os.Kill
	switch job.ShutdownSig {
	case "SIGINT", "Interrupt":
		sig = os.Interrupt
	case "SIGKILL", "Kill":
		sig = os.Kill
	}

	// FIXME:  THIS NEEDS TO BE ONLY *NIX
	pgid, err := syscallGetpgid(job.pid)
	if err != nil {
		ServerLogger.Printf("[stopJob] error: %s", err.Error())
	}
	if err = syscallKill(-pgid, sig.(syscall.Signal)); err != nil {
		ServerLogger.Printf("[stopJob] error: %s", err.Error())
	}
	job.pid = 0
	job.Pid = job.pid
	job.lock.Unlock()
	job.setRetryAttempt(0)
	//log.Printf("[endAtTime] %s completed for %s:%s:%s !", caller, job.JobUUID, job.RunUUID, job.Name)
	return true
}
