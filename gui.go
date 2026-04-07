package rpeat

import (
	"encoding/base64"
	"fmt"
	"html/template"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func CreateIcons(dir string) {
	err := os.MkdirAll(dir, os.FileMode(0770))
	if err != nil {
		fmt.Println(err)
	}
	for icon, _ := range Icons {
		err := generateIcon(icon, dir)
		if err != nil {
			fmt.Println(err)
		}
	}
}

func generateIcon(icon, path string) error {
	b64string := Icons[icon]
	var suffix string
	if icon == "favicon" {
		suffix = ".ico"
	} else {
		suffix = ".png"
	}
	file := filepath.Join(path, icon+suffix)
	exists, err := FileExists(file)
	if exists {
		return err
	}
	b, err := base64.StdEncoding.DecodeString(b64string)
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(file, b, os.FileMode(0644))
	return err
}

func CreateSounds(dir string) {
	err := os.MkdirAll(dir, os.FileMode(0770))
	if err != nil {
		fmt.Println(err)
	}
	for sound, _ := range Sounds {
		err := generateSounds(sound, dir)
		if err != nil {
			fmt.Println(err)
		}
	}
}

func generateSounds(sound, path string) error {
	b64string := Sounds[sound]
	suffix := ".wav"
	file := filepath.Join(path, sound+suffix)
	exists, err := FileExists(file)
	if exists {
		return err
	}
	b, err := base64.StdEncoding.DecodeString(b64string)
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(file, b, os.FileMode(0644))
	return err
}

var ClientHeaderHTML = `
<div id="top-banner"></div>
<div id="nav">
  <div id="nav_left">
    <div>
        <span id="servername"><a href='/'>{{ .Config.Name }}</a></span>
    </div>
    <div>
        <img id="poweredby" src="/assets/poweredbyrpeat.png" />
    </div>
  </div>
  <div id="nav_right">
    <div style="text-align: right;">
      <span id="servertime"></span>
      <button class="server-button animate-pulse" id="connect">connecting...</button>
    </div>
    <div style="text-align: right;">
        <button class=server-button style='border: 1px solid grey; background:transparent;'><a style="color:inherit; text-decoration:none;" href="https://rpeat.io/docs" target="_blank">rpeat.io docs</a></button>
        <button class=server-button onclick="reqServerInfo();">server details</button>
        <button class=server-button onclick="reqServerRestart();">reload server</button>
    </div>
  </div>
</div>
`

var JobViewHTML = `
<!DOCTYPE html>
<html lang="en">
<head>
<meta name="viewport" content="width=device-width, initial-scale=1">
<link rel="icon" type="image/x-icon" href="/assets/favicon.ico" />
<meta charset="utf-8"/>
<title>{{ .Job.Name }}</title>
<style>
  {{ template "ClientCSS" }}
</style>

<script>
  {{ template "ClientJS" . }}
</script>

</head>
<body>

<div id="dashboard">
  {{ template "ClientHeaderHTML" . }}
  <div class=jobview id="{{ .Job.JobUUID }}">
  {{ template "JobView" . }}
  </div>
</div>

<script>
  <!-- client ws js -->
  {{ template "ws" . }}
</script>

</body>
</html>`

var JobsHTML = `
<!DOCTYPE html>
<html lang="en">
<head>
<meta name="viewport" content="width=device-width, initial-scale=1">
<link rel="icon" type="image/x-icon" href="/assets/favicon.ico" />
<meta charset="utf-8"/>
<title>{{ .Config.Name }}</title>
<style>
  {{ template "ClientCSS" }}
</style>

<script>
  {{ template "ClientJS" . }}
</script>

</head>
<body>


<div id="dashboard">
   {{ template "ClientHeaderHTML" . }}
   <div class=job-table>
   {{ template "JobsTable" . }}
   </div>
</div>

<script>
  <!-- client ws js -->
  {{ template "ws" . }}
</script>


<div id="popup" onclick="showPopup();">
    <div id="popup-content" class="show-popup">
    </div>
</div>

<div id="info"></div>
</body>
</html>`

func HistoryBars(job Job) template.HTML {
	bars := make([]string, 0, len(job.History))
	for _, h := range job.History {
		if !h.isNull() {
			jobstate := fmt.Sprintf(`<img src="/assets/%s.png" alt="%s">`, h.JobStateString, h.JobStateString)
			bars = append(bars, fmt.Sprintf(`<tr class=history-row id="%s" onclick="openLog('%s','%s',100);"><td class=runuuid>%s</td><td class=nextstart>%s</td><td class=nextstart>%s</td><td class=elapsed>%s</td><td class="kstate k%s">%s</td><td>%d</td><td>view log</td></tr>`,
				h.RunUUID, job.JobUUID.String(), h.RunUUID, h.RunUUID, h.Start, h.Stop, h.Elapsed, h.JobStateString, jobstate, h.ExitCode))
		} else {
			bars = append(bars, `<tr class=history-row><td class=runuuid></td><td class=nextstart></td><td class=nextstart></td><td class=elapsed></td><td class="kstate"></td><td></td><td></td></tr>`)
		}
	}
	return template.HTML(strings.Join(bars, "\n"))
}

func DateEnvEval(job Job) template.HTML {
	DateEnv := make([]string, 0)
	loc, _ := time.LoadLocation(job.Timezone)
	ns, err := time.ParseInLocation("2006-01-02 15:04:05", job.NextStart, loc)
	if err != nil {
		ns = time.Now()
	}
	asof := ns.Format("20060102150405")
	ServerLogger.Printf("DateEnvEval - tz:%s|nextstart:%s|asof:%s", job.Timezone, job.NextStart, asof)
	for _, keyval := range job.DateEnv {
		kv := strings.Split(string(keyval), "=")
		if len(kv) != 2 {
			continue
		}
		dname := kv[0]
		dval := kv[1]
		dt, _ := ConvertDate(dval, job.Timezone, job.CalendarDirs, asof)
		dt = fmt.Sprintf("%s=%s", dname, dt)
		ServerLogger.Printf("DateEnv: %s", dt)
		DateEnv = append(DateEnv, dt)
	}
	return template.HTML(strings.Join(DateEnv, ","))
}
func GetElapsed(job Job) template.HTML {
	elapsed := fmt.Sprintf("<td class=elapsed>%s</td>", strings.ReplaceAll(job.History[0].Elapsed, " ", "&nbsp;"))
	return template.HTML(elapsed)
}
func GetControls(job Job, perms map[string]bool) template.HTML {
	availableCtls := job.availableControls()
	allCtls := []string{"hold", "start", "stop", "restart"}
	ctls := make([]string, len(allCtls))

	for i, ctl := range allCtls {
		if !perms[ctl] {
			ctls[i] = fmt.Sprintf("<td class='controls-off %s'><button class='%s-button'></button></td>", ctl, ctl)
		} else {
			if stringInSlice(ctl, availableCtls) {
				if ctl == "hold" && job.getHold() {
					ctls[i] = fmt.Sprintf("<td class=\"controls-on %s\" onclick=\"msgJob('%s','%s')\"><button class='resume-button '></button></td>", ctl, job.JobUUID, ctl)
				} else {
					ctls[i] = fmt.Sprintf("<td class='controls-on %s' onclick=\"msgJob('%s','%s')\"><button class='%s-button '></button></td>", ctl, job.JobUUID, ctl, ctl)
				}
			} else {
				ctls[i] = fmt.Sprintf("<td class='controls-off %s'><button class='%s-button'></button></td>", ctl, ctl)
			}
		}
	}
	return template.HTML(strings.Join(ctls, ""))
}

func GetDependencies(job Job, sd *ServerData) template.HTML {
	d := job.getDependencyGraph(sd)
	return template.HTML(d.HTML(true, "", 0, ""))
}

var JobView = `
<div class=job-summary>
<div id="controls" class="job-controls" style='magin: 0 auto;'>
<table class="jobsgroup" id="group_{{ .Job.Group }}">
{{ $group := index .Job.Group 0 }}
<tr class="group"><th colspan=12>{{ stringifyHTML $group }} > {{ .Job.Name }} ( {{ .Job.JobUUID }} )</th></tr>
<tr class="header">
  <th class=name>Name</th>
  <th class=details>Details</th>
  <th class=nextstart>Next</th>
  <th class=timezone>Timezone</th>
  <th class=started></th>
  <th class=kstate></th>
  <th class=elapsed>Elapsed</th>
  <th class="controls" colspan=4>Controls</th>
  <th class=viewlog>view log</th>
</tr>
<tr class="job" id="{{ .Job.JobUUID }}">
  <td style="text-align: left;" class=name>{{ .Job.Name }}</td>
  <td class="details dropdown">
     <a class=details><button class='inspect-button'></button></a>
     <div class=dropdown-content>
       <a class=jobid><b>Job ID</b>: {{ .Job.JobUUID }}</a>
       <a class=runid><b>Run ID</b>: {{ .Job.RunUUID }}</a>
       <a class=pid><b>Process ID:</b>{{ .Job.Pid }}</a>
       <a class=retryattempt><b>Retry Attempt</b>:{{ .Job.RetryAttempt }} / Retries:{{ .Job.Retry }}</a>
       <a class=status><b>Status</b>:{{ .Job.JobState }}</a>
     </div>
  <td class="nextstart dropdown">
    <a class=nextstart>{{ .Job.NextStart }}</a>
    <div class=dropdown-content>
    {{if eq .Job.NextStart "@depends"}}
      <a class=dependency style="text-align: left;">{{ getDependencies .Job }}</a>
    {{else}}
      <a class=cronstart><b>CronStart</b>: {{ .Job.CronStart }}</a>
      <a class=cronend><b>CronEnd</b>: {{ .Job.CronEnd }}</a>
      <a class=cronrestart><b>CronRestart</b>: {{ .Job.CronRestart }}</a>
    {{end}}
    </div>
  </td>
  <td class=timezone>{{ .Job.Timezone }}</td>
  <td class=started data-unix={{ .Job.StartedUNIX }}>{{ .Job.Started }}</td>
  <td class="kstate k{{ .Job.JobState }}">
    <span class=dropdown>
      <img src="/assets/{{ .Job.JobState }}.png" alt="{{ .Job.JobState }}">
    </span>
  </td>
  {{ $perms := index .Authorized .UUID }}
  {{ getElapsed .Job }}
  {{ getControls .Job $perms }}
  <td class=stdout-tail onclick='tailLog("{{ .Job.JobUUID }}","{{ .Job.RunUUID }}", 100, true, true)'>view</td>
  </td>
</tr>
</table>
</div>
<div id="history" class="job-history">
  <table class=history-table id="{{ .Job.JobUUID }}-history">
    <tr>
      <th style="width: 30%;">RunUUID</th>
      <th style="width: 20%;">Started</th>
      <th style="width: 20%;">Stopped</th>
      <th style="width: 10%;">Elapsed</th>
      <th style="width: 4%;">Status</th>
      <th style="width: 8%;">ExitCode</th>
      <th style="width: 8%;">View</th>
    </tr>
    {{ historyBars .Job }}
  </table>
</div>

<div id="logs" class="job-logs">
  <span style='font-family: sans-serif; font-size: 80%;'>stdout </span>
  <button class="server-button" style='border: 1px solid orange; background:transparent;'>
    <a id="job-stdout-link" style="color:inherit; text-decoration:none;" href="/api/log/out/{{ .Job.JobUUID }}/{{ .Job.RunUUID }}">open</a>
  </button>
  <button class="server-button" style='border: 1px solid orange; background:transparent;'>
    <a id="job-stdout-dl" style="color:inherit; text-decoration:none;" href="/api/log/out/{{ .Job.JobUUID }}/{{ .Job.RunUUID }}?download=true">download</a>
  </button>
  <div id="stdout" class="log-window job-stdout"></div>

  <span style='font-family: sans-serif; font-size: 80%;'>stderr </span>
  <button class="server-button" style='border: 1px solid orange; background:transparent;'>
    <a id="job-stderr-link" style="color:inherit; text-decoration:none;" href="/api/log/err/{{ .Job.JobUUID }}/{{ .Job.RunUUID }}">open</a>
  </button>
  <button class="server-button" style='border: 1px solid orange; background:transparent;'>
    <a id="job-stderr-dl" style="color:inherit; text-decoration:none;" href="/api/log/err/{{ .Job.JobUUID }}/{{ .Job.RunUUID }}?download=true">download</a>
  </button>
  <div id="stderr" class="log-window job-stderr"></div>
</div>

<table id="info" class=job-info>
  <tr class=group><th colspan=2>Configuration</th></tr>
  <tr class=header><th style="width: 20%;"></th><th style="width: 80%;"></th></tr>
  <tr><td>Cmd</td><td> {{ .Job.Cmd }}</td></tr>
  <tr><td>Cmd (Evaluated)</td><td> {{ .Job.CmdEval }}</td></tr>
  <tr><td>Description</td><td> {{ .Job.Description }}</td></tr>
  <tr><td>Comment</td><td> {{ .Job.Comment }}</td></tr>
  <tr><td>Tags</td><td> {{ stringify .Job.Tags }}</td></tr>
  <tr><td>Env</td><td> {{ stringify .Job.Env }}</td></tr>
  <tr><td>DateEnv</td><td> {{ stringify .Job.DateEnv }}</td></tr>
  <tr><td>DateEnv (Evaluated)</td><td> {{ DateEnvEval .Job }}</td></tr>
  <tr><td>Inherits</td><td> {{ stringifyWithSep .Job.InheritanceChain " =>"}}</td></tr>
  <tr><td>Group</td><td> {{ stringify .Job.Group }}</td></tr>
  <tr><td>Type</td><td> {{ .Job.Type }}</td></tr>
  <tr><td>User</td><td> {{ .Job.User }}</td></tr>
  <tr><td>Admin</td><td> {{ stringify .Job.Admin }}</td></tr>
  <tr><td>Permissions</td><td> {{ stringify .Job.Permissions }}</td></tr>
  <tr><td>Disabled</td><td> {{ .Job.Disabled }}</td></tr>
  <tr><td>Hidden</td><td> {{ .Job.Hidden }}</td></tr>
  <tr><td>CronStart</td><td> {{ .Job.CronStart }}</td></tr>
  <tr><td>CronEnd</td><td> {{ .Job.CronEnd }}</td></tr>
  <tr><td>CronRestart</td><td> {{ .Job.CronRestart }}</td></tr>
  <tr><td>StartDay</td><td> {{ .Job.StartDay }}</td></tr>
  <tr><td>StartTime</td><td> {{ .Job.StartTime }}</td></tr>
  <tr><td>EndDay</td><td> {{ .Job.EndDay }}</td></tr>
  <tr><td>EndTime</td><td> {{ .Job.EndTime }}</td></tr>
  <tr><td>Timezone</td><td> {{ .Job.Timezone }}</td></tr>
  <tr><td>Calendar</td><td> {{ .Job.Calendar }}</td></tr>
  <tr><td>CalendarDirs</td><td> {{ stringify .Job.CalendarDirs }}</td></tr>
  <tr><td>StartRule</td><td> {{ .Job.StartRule }}</td></tr>
  <tr><td>ShutdownCmd</td><td> {{ .Job.ShutdownCmd }}</td></tr>
  <tr><td>ShutdownCmd (Evaluated)</td><td> {{ .Job.ShutdownCmdEval }}</td></tr>
  <tr><td>StdoutFile</td><td> {{ stringify .Job.StdoutFile }}</td></tr>
  <tr><td>StderrFile</td><td> {{ stringify .Job.StderrFile }}</td></tr>
  <tr><td>Retry</td><td> {{ .Job.Retry }}</td></tr>
  <tr><td>RetryWait</td><td> {{ .Job.RetryWait }}</td></tr>
  <tr><td>RetryReset</td><td> {{ .Job.RetryReset }}</td></tr>
  <tr><td>MissedReset</td><td> {{ .Job.MissedReset }}</td></tr>
  <tr><td>MaxDuration</td><td> {{ .Job.MaxDuration }}</td></tr>
</table>
</div>

<!-- https://www.w3schools.com/w3css/tryit.asp?filename=tryw3css_tabulators_close -->
<script>
  updateElapsed("{{ .Job.JobUUID }}");
  function openTab(tabName) {
    var i;
    var x = document.getElementsByClassName("log-window");
    for (i = 0; i < x.length; i++) {
      x[i].style.display = "none";  
    }
    document.getElementById(tabName).style.display = "block";  
  }
  tailLog("{{ .Job.JobUUID }}","{{ .Job.RunUUID }}", 100, true, true);
</script>
`

var JobsTable = `
{{ range $group := .GroupOrder }}
{{ $jobs := index $.Jobs $group }} 
<table class="jobsgroup" id="group_{{ $group }}">
<tr class="group"><th colspan=30><a style='text-decoration:none;' href="{{ $.Base }}/{{ stringifyHTML $group }}">{{ stringifyHTML $group }}</a></th></tr>
<tr class="header">
  <th class=name>Name</th>
  <th class=details>Details</th>
  <th class=nextstart>Next</th>
  <th class=timezone>Timezone</th>
  <th class=started></th>
  <th class=kstate></th>
  <th class=elapsed>Elapsed</th>
  <th class="controls" colspan=4>Controls</th>
  <th class=history-trail colspan=7>History</th>
</tr>
{{ $authorized := $.Authorized }}
{{ range $id := index $.JobOrder $group }}
  {{ $job := index $jobs $id }}
  {{ $perms := index $authorized $id }}
    <tr class="job" id="{{ $job.JobUUID }}">
      <td style="text-align: left;" class=name>{{ $job.Name }}</td>
      <td class="details dropdown">
         <a class=details target="_blank" href="{{ $.Base }}/job/{{ slugify $job.Name }}"><button class='inspect-button'></button></a>
         <div class=dropdown-content>
           <a class=jobid><b>Job ID</b>: {{ $job.JobUUID }}</a>
           <a class=runid><b>Run ID</b>: {{ $job.RunUUID }}</a>
           <a class=pid><b>Process ID:</b>{{ $job.Pid }}</a>
           <a class=retryattempt><b>Retry Attempt</b>:{{ $job.RetryAttempt }} / Retries:{{ $job.Retry }}</a>
           <a class=status><b>Status</b>:{{ $job.JobState }}</a>
         </div>
      </td>
      <td class="nextstart dropdown">
        <a class=nextstart>{{ $job.NextStart }}</a>
        <div class=dropdown-content>
        {{if eq $job.NextStart "@depends"}}
          <a class=dependency style="text-align: left;">{{ getDependencies $job }}</a>
        {{else}}
          <a class=cronstart><b>CronStart</b>: {{ $job.CronStart }}</a>
          <a class=cronend><b>CronEnd</b>: {{ $job.CronEnd }}</a>
          <a class=cronrestart><b>CronRestart</b>: {{ $job.CronRestart }}</a>
        {{end}}
        </div>
      </td>
      <td class=timezone>{{ $job.Timezone }}</td>
      <td class=started data-unix={{ $job.StartedUNIX }}>{{ $job.Started }}</td>
      <td class="kstate k{{ $job.JobState }}">
        <span class=dropdown>
          <img src="/assets/{{ $job.JobState }}.png" alt="{{ $job.JobState }}">
        </span>
      </td>
      {{ getElapsed $job }}
      {{ getControls $job $perms }}
      <td class=history-trail>{{ range $history := $job.History }}<span class=dropdown><img id="{{ $history.RunUUID }}" src="/assets/{{ $history.JobStateString }}.png" alt="{{ $history.JobStateString }}"><div class='dropdown-content'><div>state: {{$history.JobStateString}}</div><hr/><div>start: {{$history.Start}}</div><div>stop: {{$history.Stop}}</div><div>elapsed: {{$history.Elapsed}}</div><div>unscheduled: {{$history.Unscheduled}}</div><div>exit: {{$history.ExitCode}}</div></div></span>{{ end }}
      </td>
    </tr>
    <script>newJob("{{ $job.JobUUID }}",{{ $job.History }});</script>
    <script>updateElapsed("{{ $job.JobUUID }}");</script>
{{ end }}
</table>
{{ end }}`

var ClientJS = `
// https://stackoverflow.com/questions/51689653/how-to-smoothly-scroll-to-an-element-in-pure-javascript
window.smoothScroll = function(target, speed=10) {
  var scrollContainer = target;
  do { //find scroll container
    scrollContainer = scrollContainer.parentNode;
    if (!scrollContainer) return;
    scrollContainer.scrollTop += 1;
  } while (scrollContainer.scrollTop == 0);

  var targetY = 0;
  do { //find the top of target relatively to the container
    if (target == scrollContainer) break;
    targetY += target.offsetTop;
  } while (target = target.offsetParent);

  scroll = function(c, a, b, i) {
    i++;
    if (i > 30) return;
    c.scrollTop = a + (b - a) / 30 * i;
    setTimeout(function() {
      scroll(c, a, b, i);
    }, speed);
  }
  // start scrolling
  scroll(scrollContainer, scrollContainer.scrollTop, targetY, 0);
}

function openLog(jobid, runid, n, err=true, out=true) {
  tailLog(jobid, runid, n, err, out);
}

function dhms(t) {
  var d = 0;
  var h = 0;
  var m = 0;

  if (t >= 86400) {
    d = Math.trunc(t / 86400);
  };
  if (t >= 3600) {
    h = Math.abs(d*24 - Math.trunc(t /3600));
  };
  if (t >= 60) {
    m = Math.trunc(t/60) - (Math.trunc(t/3600) * 60);
  };
  var s = t % 60;

  ctime = [d,h,m,s];
  var sigtime = Math.min(ctime.findIndex(x => x != 0),3);
  if (sigtime == -1) {
    sigtime = 3;
  }
  let labels = ["d","h","m","s"].slice(sigtime, sigtime+2);
  let times = ctime.slice(sigtime,sigtime+2);
  let time;
  if (sigtime == 3) {
    time = times[0].toString().padStart(6)+labels[0];
  } else {
    time = times[0].toString().padStart(2)+labels[0]+" "+times[1].toString().padStart(2)+labels[1];
  }
  return time.replaceAll(" ", "&nbsp;");
}

/*
openWindowWithPost("http://account.rpeat.io/", {
    p: "view.map",
    coords: encodeURIComponent(coords)
});
*/
function openWindowWithPost(url, data) {
    var form = document.createElement("form");
    form.target = "_blank";
    form.method = "POST";
    form.action = url;
    form.style.display = "none";

    for (var key in data) {
        var input = document.createElement("input");
        input.type = "hidden";
        input.name = key;
        input.value = data[key];
        form.appendChild(input);
    }

    document.body.appendChild(form);
    form.submit();
    document.body.removeChild(form);
}


var popup_visible = false;
function showPopup() {
    if(popup_visible === false) {
      document.getElementById("dashboard").style.filter = "blur(2px)";
      document.getElementById("dashboard").style.webkitFilter = "blur(2px)";
      document.getElementById("popup").style.display = "block";
      document.getElementById("popup").style.visibility = "visible";
    } else {
      document.getElementById("dashboard").style.filter = "blur(0px)";
      document.getElementById("dashboard").style.webkitFilter = "blur(0px)";
      document.getElementById("popup").style.display = "none";
      document.getElementById("popup").style.visibility = "hidden";
    }
    popup_visible = !popup_visible;
}


function reqStatus(jobid) {
  var xhttp = new XMLHttpRequest();
  xhttp.onreadystatechange = function() {
    if (this.readyState == 4 && this.status == 200) {
       let obj = JSON.parse(xhttp.responseText);
       localStorage.setItem("status_"+jobid, JSON.stringify(obj));
    };
  };
  xhttp.open("POST", "{{ .Base }}/api/status", true);
  xhttp.setRequestHeader("Content-Type", "application/json;charset=UTF-8");
  xhttp.send(JSON.stringify({"jobid":jobid}));
}
function reqInfo(jobid) {
  var xhttp = new XMLHttpRequest();
  xhttp.onreadystatechange = function() {
    if (this.readyState == 4 && this.status == 200) {
       let obj = JSON.parse(xhttp.responseText);
       localStorage.setItem("info_"+jobid, JSON.stringify(obj));
    };
  };
  xhttp.open("POST", "{{ .Base }}/api/info", true);
  xhttp.setRequestHeader("Content-Type", "application/json;charset=UTF-8");
  xhttp.send(JSON.stringify({"jobid":jobid}));
}
function reqDependencies(jobid) {
  var xhttp = new XMLHttpRequest();
  xhttp.onreadystatechange = function() {
    if (this.readyState == 4 && this.status == 200) {
       let obj = JSON.parse(xhttp.responseText);
       localStorage.setItem("dep_"+jobid, JSON.stringify(obj));
    };
  };
  xhttp.open("POST", "{{ .Base }}/api/dependencies", true);
  xhttp.setRequestHeader("Content-Type", "application/json;charset=UTF-8");
  xhttp.send(JSON.stringify({"jobid":jobid}));
}

function dependenciesByLevel(d) {
  let deps = [];
  let deps_tmp = {};
  d.job.Dependencies.forEach( (x) => {
    for(const [k,v] of Object.entries(x.triggers)) {
      deps_tmp[k] = v;
    }
  })
  deps.push(deps_tmp);
  let hasdep = true;
  while (hasdep) {
    deps_tmp = {};
    for( const k of Object.keys(deps[deps.length-1]) ) {
      if(deps[deps.length-1][k].Dependencies !== null) {
        deps[deps.length-1][k].Dependencies.forEach( (x) => {
          for(const [k,v] of Object.entries(x.triggers)) {
            deps_tmp[k] = v;
          }
        })
      } else {
        hasdep = false;
        console.log("no more dependencies");
      }
    }
    if(hasdep) deps.push(deps_tmp);
  }
  return deps;
}
function getDependencies(d, name="job", lpad=0) {
  let job = d[name];
  if (job.Dependencies === null ) return null;
    if(name=="job") {
      console.log(job.Name+" ("+job.JobUUID+")");
    }
    lpad = 4+lpad;
    job.Dependencies.forEach( (x) => {
    console.log(" ".repeat(lpad)+" Action:"+x.action+" Requires:"+x.condition+"");
    for( const [uuid,t] of Object.entries(x.triggers) ) {
      console.log(" ".repeat(lpad)+"  \u2196 "+t.Name+" ("+x.triggerUUIDs[uuid]+")");
      if (t.Dependencies !== null) {
        getDependencies(x.triggers, t.JobUUID, lpad=1*lpad);
      }
    }
  })
}
function getDependenciesAsHTML(d, name="job", lpad=1, html="") {
  let job = d[name];
  if (job.Dependencies === null ) return null;
    if(name=="job") {
      //html = html+"<br/><div>&nbsp;<b>"+job.Name+"</b> <span style='padding-left: 3ch; color: #777'>("+job.JobUUID+")</span></div>\n";
      html = html+"<br/><div>&nbsp;<b>"+job.Name+"</b></div>";
    }
    lpad = 4+lpad;
    job.Dependencies.forEach( (x) => {
    html = html+"<div style='color: orange; padding-top:0.6ch;'>"+"&nbsp;".repeat(lpad)+"&nbsp;&nbsp;Action: <span style='color:#777'>"+x.action+"</span> | Requires: <span style='color:#777'><i>"+x.condition+"</i></span></div>";
    for( const [uuid,t] of Object.entries(x.triggers) ) {
      console.log(lpad)
      html = html+"<div>"+"&nbsp;".repeat(lpad)+"&nbsp;&nbsp;&nbsp;<b style='padding-left: 1ch; color: #555;'>&nwarr;</b>&nbsp;&nbsp;&nbsp;<b><span data-dep-jobuuid='"+uuid+"'>"+t.Name+"</b> <span style='padding-left: 2ch; color:#BBB'>trigger:<span style='color: #999;'><b>"+x.triggerUUIDs[uuid]+"</b></span></div>\n";
      //html = html+"<div>"+"&nbsp;".repeat(lpad)+"&nbsp;&nbsp;&nwarr;&nbsp<b>"+t.Name+"</b></div>";
      if (t.Dependencies !== null) {
        html = getDependenciesAsHTML(x.triggers, t.JobUUID, lpad=1*lpad, html);
      }
    }
  })
  return html + "<br/>";
}
function renderDependencies(jobid) {
  let deps = JSON.parse(localStorage.getItem("dep_"+jobid));
  return deps;
}




var server_status;
var servertimeUNIX;
function reqServerInfo() {
  var xhttp = new XMLHttpRequest();
  xhttp.onreadystatechange = function() {
    if (this.readyState == 4 && this.status == 200) {
      var obj = JSON.parse(xhttp.responseText);
      server_status = obj;
      let count = {ready:0,onhold:0,retrywait:0,failed:0,end:0,success:0,manualsuccess:0,running:0,depwarning:0,depfailed:0,missedwarning:0,warning:0,stopped:0,allsuccess:0};
      Object.entries(server_status.jobs).forEach(([k,v]) => {let s = v.JobStateString; count[s]++;})
      let njobs = Object.values(count).reduce((x,s) => x+s);
      count.allsuccess = (count.success+count.manualsuccess+count.end);
      Object.entries(count).forEach( ([k,v]) => { count[k] = v > 0 ? "<span class=server-status-count>"+v+"</span>" : "" } );
      var info = "<div>Server Details<hr><br></div>";
      info += "<table id='server-details'>";
      info += "<tr class=server-details-header><td colspan=2 >running</td><td colspan=2>retrying</td><td colspan=2>holding</td><td colspan=2>TOTAL</td></tr>";
      info += "<tr>";
      info += "<td><img src='/assets/running.png' alt=''></td><td>"+count.running+"</td>";
      info += "<td><img src='/assets/retrywait.png' alt=''></td><td>"+count.retrywait+"</td>";
      info += "<td><img src='/assets/onhold.png' alt=''></td><td>"+count.onhold+"</td>";
      info += "<td><img src='/assets/ready.png' alt=''></td><td>"+njobs+"</td>";
      info += "</tr>";
      info += "<tr class=server-details-header><td colspan=2 >success</td><td colspan=2>failed</td><td colspan=2>stopped</td><td colspan=2>warning</td></tr>";
      info += "<tr>";
      info += "<td><img src='/assets/success.png' alt=''></td><td>"+count.allsuccess+"</td>";
      info += "<td><img src='/assets/failed.png' alt=''></td><td>"+count.failed+"</td>";
      info += "<td><img src='/assets/stopped.png' alt=''></td><td>"+count.stopped+"</td>";
      info += "<td><img src='/assets/warning.png' alt=''></td><td>"+count.warning+"</td>";
      info += "</tr>";
      info += "<tr class=server-details-header><td colspan=2 ></td><td colspan=2>dep failed</td><td colspan=2>dep warning</td><td colspan=2>missed</td></tr>";
      info += "<tr>";
      info += "<td></td><td></td>";
      info += "<td><img src='/assets/depfailed.png' alt=''></td><td>"+count.depfailed+"</td>";
      info += "<td><img src='/assets/depwarning.png' alt=''></td><td>"+count.depwarning+"</td>";
      info += "<td><img src='/assets/missedwarning.png' alt=''></td><td>"+count.missedwarning+"</td>";
      info += "</tr>";
      info += "</table>";
      document.getElementById("popup-content").innerHTML = info;
	  showPopup();
    };
  };
  xhttp.open("POST", "{{ .Base }}/api/jobs/status", true);
  xhttp.setRequestHeader("Content-Type", "application/json;charset=UTF-8");
  xhttp.send("{}");
};
function reqServerRestart() {
  var xhttp = new XMLHttpRequest();
  xhttp.onreadystatechange = function() {
    if (this.readyState == 4 && this.status == 200) {
       var obj = JSON.parse(xhttp.responseText)
       console.log(obj);
       //document.getElementById("info").innerHTML = "<pre>"+JSON.stringify(obj, null, 2)+"</pre>";
    };
  };
  xhttp.open("POST", "{{ .Base }}/api/serverrestart", true);
  xhttp.setRequestHeader("Content-Type", "application/json;charset=UTF-8");
  xhttp.send("{}");
};
function tailLog(jobid, runid, lines, err, out) {
  var xhttp = new XMLHttpRequest();
  xhttp.onreadystatechange = function() {
    if (this.readyState == 4 && this.status == 200) {
       var obj = JSON.parse(xhttp.responseText);
       var job = obj["job"];
       var j = document.getElementById(jobid);
       console.log(job);

       document.getElementById("job-stdout-link").setAttribute("data-file",job["StdoutFile"]);
       document.getElementById("job-stdout-link").setAttribute("href","/api/log/out/"+jobid+"/"+runid);
       document.getElementById("job-stdout-dl").setAttribute("href","/api/log/out/"+jobid+"/"+runid+"?download=true");
       var stdout = j.querySelector("div.job-stdout");
       stdout.innerHTML = "<pre>" + job["Stdout"] + "</pre>";

       document.getElementById("job-stderr-link").setAttribute("data-file",job["StderrFile"]);
       document.getElementById("job-stderr-link").setAttribute("href","/api/log/err/"+jobid+"/"+runid);
       document.getElementById("job-stderr-dl").setAttribute("href","/api/log/err/"+jobid+"/"+runid+"?download=true");
       var stderr = j.querySelector("div.job-stderr");
       stderr.innerHTML = "<pre>" + job["Stderr"] + "</pre>";
    };
  };
  xhttp.open("POST", "{{ .Base }}/api/log", true);
  xhttp.setRequestHeader("Content-Type", "application/json;charset=UTF-8");
  xhttp.send(JSON.stringify({ "jobid": jobid, "runid": runid, "stdout": out, "stderr": err, "lines": lines}));
};

function updateField(obj, query) {
    var o = obj.querySelector(query);
    if (o !== null) {
        return o;
    } else {
        var o = {innerHTML:{}, className:{}, data:{unix:{}}};
        return o;
    }

}
function updateInnerHTML(field, value) {
    if (field !== null) {
        field.innerHTML = value
    }
}
function updateClassName(field, value) {
    if (field !== null) {
        field.className = value
    }
}

function msgJob(id, type) {
  var xhttp = new XMLHttpRequest();
  xhttp.onreadystatechange = function() {
    if (this.readyState == 4 && this.status == 200) {
       var obj = JSON.parse(xhttp.responseText)
       //console.log(obj)
       //if (!job.hasOwnProperty("JobUUID")) {
       //  return
       //}
    }
  };
  updateControls(id, null)
  xhttp.open("POST", "{{ .Base }}/api/"+type, true);
  xhttp.setRequestHeader("Content-Type", "application/json;charset=UTF-8");
  xhttp.send(JSON.stringify({ "jobid": id }));
};

function jobstateAbb(stateString) {
      switch (stateString) {
        case "success":
          symbol = "S";
          break;
        case "failed":
          symbol = "F";
          break;
        case "retryfailed":
          symbol = "R";
          break;
        case "end":
          symbol = "E";
          break;
        case "stopped":
          symbol = "s";
          break;
        case "missed":
          symbol = "m";
          break;
      }
      return symbol;
}

function updateHistoryTable(job) {}

function updateHistory(id, job) {
  if (job === null) {
     return;
  }
  var inner = "";
  var symbol;
  job.forEach(function(element) {
      var e = document.getElementById(id)
      if (element.JobStateString != "") {
        var jss = element.JobStateString;
        e.querySelector(".history-trail").innerHTML = inner + "<span class=dropdown id='"+element.RunUUID+"'><img src='/assets/"+jss+".png' alt=''><div class='dropdown-content'><div>state: "+jss+"</div><hr/><div>start: "+element.Start+"</div><div>stop: "+element.Stop+"</div><div>elapsed: "+element.Elapsed+"</div><div>unscheduled: "+element.Unscheduled+"</div><div>exitCode: "+element.ExitCode+"</div></div></span>";
        inner = e.querySelector(".history-trail").innerHTML;
      }
  });
};
function getHistory(id, runid) {
  var historyel = JSON.parse(localStorage.getItem(id))[0];
  var info = historyel.filter(function(j) {return j.RunUUID == runid;});
  document.getElementById("info").innerHTML = "<pre>"+JSON.stringify(info, null, 2)+"</pre>";
}
function getInfo(id) {
  document.getElementById("info").innerHTML = "<pre>"+id+"</pre>";
}
function sleep(ms) {
  return new Promise(resolve => setTimeout(resolve, ms));
}
function dhmsSince(obj, el) {
    startUNIX = el.StartUNIX
    //console.log(Math.ceil(Math.round((Date.now() - startUNIX * 1000) / 1000)));
    var since = dhms(Math.ceil(Math.round((Date.now() - startUNIX * 1000) / 1000)));
    dd = obj.childNodes
    //dd.innerHTML = "<div class='dropdown-content'><div>Start:"+el.Start+"("+since+"</div></div>";
}
function updateElapsed(id) {
    var e = document.getElementById(id);
    var started = parseInt(e.querySelector("td.started").dataset.unix) * 1000;
    if (e.querySelector("td.kstate").className == "kstate krunning") {
      var ival = setInterval(function() {
        if ( !isNaN(servertimeUNIX) ) {  // protect against delayed ws
          var elapsed = dhms(Math.ceil(Math.round((servertimeUNIX - started) / 1000)));
          e.querySelector("td.elapsed").style.opacity ="100%";
          e.querySelector("td.elapsed").innerHTML = elapsed;
        }

        if (e.querySelector("td.kstate").className != "kstate krunning") {
          clearInterval(ival)
          e.querySelector("td.elapsed").style.opacity ="25%";
        }
      }, 100)
    }
}
function updateControls(id, controls) {
  var start;
  var j = document.getElementById(id);
  //var isHold = j.querySelector("td.hold").innerHTML == "hold"
  var actions = ["hold","stop","start","restart"];
  actions.forEach(function(action) {
    var a = j.querySelector("."+action);
    if (controls == null) {
        a.className = "controls-off "+action;
        a.onclick = null
    } else {
        if ( controls.includes(action) ) {
          a.className = "controls-on "+action;
          a.onclick = function() {msgJob(id, action);};
        } else {
          a.className = "controls-off "+action;
          a.onclick = null
        }
    }
  })
}
var runuuid = {};
async function updateJob(id, job, update, imgpath="assets") {
  // td. elems are in tables, a. elems are in dropdowns
  j = document.getElementById(id)
  if(j === null) return;
  updateInnerHTML(j.querySelector("a.nextstart"), job["NextStart"]);
  j.querySelector("td.started").innerHTML = job["Started"];
  j.querySelector("td.started").dataset.unix = job["StartedUNIX"];
  let jstate = job["JobStateString"];
  if ( jstate === "missed" ) {
    jstate = "onhold";
  }
  j.querySelector("td.kstate").className = "kstate k"+jstate;
  j.querySelector("td.kstate").innerHTML = '<td><span class=dropdown><img src="/assets/'+jstate+'.png" alt="'+jstate+'"><div class=dropdown-content><div>'+jstate+'</div></span></td>'
  var e = j.querySelector("a.runid");
  if (e !== null) { e.innerHTML = "Run ID: " + job["RunUUID"]; };
  e = j.querySelector("td.runid");
  if (e !== null) { e.innerHTML = job["RunUUID"]; };
  updateInnerHTML(j.querySelector("a.retryattempt"), "Retry: " + job["RetryAttempt"] + "/" + job["Retry"]);
  updateInnerHTML(j.querySelector("a.pid"), "PID: " + job["Pid"]);
  updateInnerHTML(j.querySelector("td.pid"), job["Pid"]);
  updateInnerHTML(j.querySelector("a.status"),"Status: " + job["JobStateString"]);
  e = j.querySelector("td.stdout-tail");
  if (e !== null) {
     e.onclick=function() {tailLog(job["JobUUID"],job["RunUUID"],100,true,false)}
  }
  e = j.querySelector("td.stderr-tail");
  if (e !== null) {
     e.onclick=function() {tailLog(job["JobUUID"],job["RunUUID"],100,false,true)}
  }
  if (job["Hold"] ) {
    j.querySelector("td.hold").innerHTML = "<button class='resume-button'></button>";
  } else {
    j.querySelector("td.hold").innerHTML = "<button class='hold-button'></button>";
  }
  var runstate = ["running","retrying"];
  var endstate = ["success","manualsuccess","end","failed","retrywait","retryfailed","stopped","missed"];
  var errstate = ["failed","retryfailed","retryfailed","stopped","missedwarning"];
  if (errstate.includes(job["JobStateString"])) {
    // disable audible alerts for the moment
    //playAlert();
  }
  if (job["JobStateString"] == "restart") {
    await sleep(1000)
  };
  if (job["JobStateString"] == "running") {
    //var started = Date.parse(j.querySelector(".started").innerHTML)
    var started = parseInt(j.querySelector("td.started").dataset.unix) * 1000
    updateElapsed(id);
  };
  if (endstate.includes(job["JobStateString"])) {
    j.querySelector("td.elapsed").innerHTML = "";
  };
  if (endstate.includes(job["JobStateString"])) { // && update && (job["JobStateString"] != job["PrevJobStateString"])) {
    var previousJobs = JSON.parse(localStorage.getItem(id));
    previousJobs.unshift(job["History"]);
    if (previousJobs.length > 10) {
      previousJobs.pop(1);
    }
    let MAXROWS=10;
    localStorage.setItem(id, JSON.stringify(previousJobs));
    if ( j.querySelector(".job-history") !== null && (job["JobStateString"] != job["PrevJobStateString"]) && runuuid[job["RunUUID"]] === undefined ) {
      runuuid[job["RunUUID"]] = true;
      prevjob = previousJobs[0][0];
      if(document.getElementById(job["JobUUID"]+"-history").rows.length == MAXROWS+1) {
        document.getElementById(job["JobUUID"]+"-history").deleteRow(MAXROWS);
      }
      row = document.getElementById(job["JobUUID"]+"-history").insertRow(1);
      row.className = "history-row";
      row.id = prevjob["RunUUID"];
      row.addEventListener("click",function() { openLog(id, this.id, 100); });
      var cell = row.insertCell(0);
      cell.className = "runuuid";
      cell.innerHTML = prevjob["RunUUID"];
      cell = row.insertCell(1);
      cell.className = "nextstart";
      cell.innerHTML = prevjob["Start"];
      cell = row.insertCell(2);
      cell.className = "nextstart";
      cell.innerHTML = prevjob["Stop"];
      cell = row.insertCell(3);
      cell.className = "elapsed";
      cell.innerHTML = prevjob["Elapsed"];
      cell = row.insertCell(4);
      cell.className = "kstate k"+prevjob["JobStateString"];
      cell.innerHTML = '<img src="/assets/'+jstate+'.png" alt="'+jstate+'">'
      cell = row.insertCell(5);
      cell.innerHTML = prevjob["ExitCode"];
      cell = row.insertCell(6);
      cell.innerHTML = "view log";
    }
  }
  updateControls(id, job["Controls"]);
  if (j.querySelector(".history-trail") !== null) {
    updateHistory(id, job["History"]);
  }
}
function newJob(id, history) {
  localStorage.setItem(id, JSON.stringify([history]));
};`

var ClientWS = `
{{ define "ws" }}
var connect = document.getElementById("connect");
var updated = document.getElementById("updated");
var input = document.getElementById("input");

var WS_ATTEMPTS = 0;

function startWebSocket(reload){
   console.log("startWebSocket");
    ws = new WebSocket(location.origin.replace(/^http/, 'ws')+"{{ .Base }}/api/updates");
    ws.onopen = function () {
        if (reload === true) {
          // reload entire page if connection has been lost and then reconnects
          // this handles stale data in case of lost connection
          location.reload();
        }
        connect.innerHTML = "connected";
        connect.className = "server-connected";
    };
    ws.onmessage = function (e) {
        var update = JSON.parse(e.data);
        if (update.hasOwnProperty('Uuid')) {
           console.log(update);
           updateJob(update['Uuid'], update['Job'], true, "{{ .Base }}/assets");
        }
        servertimeUNIX = update["Modified"]*1e3
        var timestamp = new Date(update["Modified"]*1e3 + update["Tzoffset"]*1e3).toISOString();
        timestamp = timestamp.replace("Z","").slice(0,-4);
        document.getElementById("servertime").innerHTML = timestamp + " " + update["Tzname"] + " ";
    };
    ws.onclose = function(){
        // Try to reconnect in 5 seconds
        connect.innerHTML = "disconnected";
        document.getElementById("servertime").innerHTML = "retrying connection ("+WS_ATTEMPTS+"/5) ";
        connect.className = "server-disconnected";
        ws=null;
        if(WS_ATTEMPTS < 5) {
          WS_ATTEMPTS++;
          setTimeout(function() { startWebSocket(true);}, 2500 * WS_ATTEMPTS);
        } else {
          document.getElementById("servertime").innerHTML = "connection failed ";
          document.getElementById("servertime").class = "";
          connect.innerHTML = "reconnect";
          connect.addEventListener("click",function() { startWebSocket(true); });
          connect.className = "server-reconnect";
        }
    };
};
startWebSocket(false);

var themes = {
  "rpeat-theme-dark":{
    "--servername-color":"white",
    "--background-color":"#2A2A2A",
    "--group-name-color":"white",
    "--row-background-color-odd":"#4A4A4A",
    "--row-color-odd":"white",
    "--row-background-color-even":"#353535",
    "--row-color-even":"white",
    "--controls-background-color":"#252525",
  },
  "rpeat-theme-light":{
    "--servername-color":"black",
    "--background-color":"white",
    "--group-name-color":"#777",
    "--row-background-color-odd":"#EFEFEF",
    "--row-color-odd":"black",
    "--row-background-color-even":"white",
    "--row-color-even":"black",
    "--controls-background-color":"initial",
  },
}
function changeTheme(theme) {
   switch (theme) {
     case "dark":
       darkMode();
       break;
     case "light":
       lightMode();
       break;
     case "green":
       greenMode();
       break;
     case "blue":
       blueMode();
       break;
     default:
       console.log("theme unavailable");
  }
}
function darkMode() {
  document.documentElement.style.setProperty("--servername-color", "white");
  document.documentElement.style.setProperty("--background-color", "#2A2A2A");
  document.documentElement.style.setProperty("--group-name-color", "white");
  document.documentElement.style.setProperty("--row-background-color-odd", "#4A4A4A");
  document.documentElement.style.setProperty("--row-color-odd", "white");
  document.documentElement.style.setProperty("--row-background-color-even", "#353535");
  document.documentElement.style.setProperty("--row-color-even", "white");
  document.documentElement.style.setProperty("--controls-background-color", "#252525");
}
function lightMode() {
  document.documentElement.style.setProperty("--servername-color", "black");
  document.documentElement.style.setProperty("--background-color", "white");
  document.documentElement.style.setProperty("--group-name-color", "#777");
  document.documentElement.style.setProperty("--row-background-color-odd", "#EFEFEF");
  document.documentElement.style.setProperty("--row-color-odd", "black");
  document.documentElement.style.setProperty("--row-background-color-even", "white");
  document.documentElement.style.setProperty("--row-color-even", "black");
  document.documentElement.style.setProperty("--controls-background-color", "initial");
}
function greenMode() {
  document.documentElement.style.setProperty("--servername-color", "black");
  document.documentElement.style.setProperty("--background-color", "honeydew");
  document.documentElement.style.setProperty("--group-name-color", "#777");
  document.documentElement.style.setProperty("--row-background-color-odd", "#EFEFEF");
  document.documentElement.style.setProperty("--row-color-odd", "black");
  document.documentElement.style.setProperty("--row-background-color-even", "honeydew");
  document.documentElement.style.setProperty("--row-color-even", "black");
  document.documentElement.style.setProperty("--controls-background-color", "initial");
}
function blueMode() {
  document.documentElement.style.setProperty("--servername-color", "white");
  document.documentElement.style.setProperty("--background-color", "#001D3D");
  document.documentElement.style.setProperty("--group-name-color", "#777");
  document.documentElement.style.setProperty("--row-background-color-odd", "#003566");
  document.documentElement.style.setProperty("--row-color-odd", "white");
  document.documentElement.style.setProperty("--row-background-color-even", "#001D3D");
  document.documentElement.style.setProperty("--row-color-even", "white");
  document.documentElement.style.setProperty("--controls-background-color", "initial");
}
function playAlert() {
  var failed = new Audio("/assets/failed.wav");
  failed.play();
}

{{ end }}`

var ClientCSS = `
/*  
https://css-tricks.com/fixing-tables-long-strings/
https://csslayout.io/nested-dropdowns/
*/

body {
    background-color: var(--background-color);
}
/* Tooltip container */
.tooltip {
  position: relative;
  display: inline-block;
  border-bottom: 1px dotted black; /* If you want dots under the hoverable text */
  opacity:100%;
  /*
  bottom: 100%;
  left: 50%;
  margin-left: -60px;
  */
}

/* Tooltip text */
.tooltip .tooltiptext {
  visibility: hidden;
  width: 80px;
  background-color: darkgrey;
  color: #fff;
  text-align: center;
  padding: 5px 0;
  border-radius: 6px;
  /* Position the tooltip text - see examples below! */
  position: absolute;
  z-index: 20;
}

/* Show the tooltip text when you mouse over the tooltip container */
.tooltip:hover .tooltiptext {
  visibility: visible;
}

#popup {
    width: 100%;
    height: 100%;
    top: 0;
    position: absolute;
    visibility: hidden;
    display: none;
    /* background-color: rgba(150,150,150,0.8); /* complimenting your modal colors */
}
#popup:target {
    visibility: visible;
    display: block;
}
.show-popup {
    background:white;
    margin: 0 auto;
    width:500px;
    position:relative;
    z-index:10;
    top: 25%;
    padding:10px;
    border: 1px solid var(--main-group-name-color);
    border-radius: 10px;
    filter: blur(0px);
    -webkit-box-shadow:0 0 10px rgba(0,0,0,0.4);
    -moz-box-shadow:0 0 10px rgba(0,0,0,0.4);
    box-shadow:0 0 10px rgba(0,0,0,0.4);
}
#server-details img {
  vertical-align: middle;
  margin-left: 5px;
}
#server-details td {
  padding-left: 5px;
  width: 50px;
}
#server-details td {
  background-color: var(--row-background-color-even);
  border-radius: unset;
}
.server-details-header td {
  color: #888;
  padding-top: 20px;
  border-bottom: 1px solid orange;
}

td.dropdown, span.dropdown, button.dropdown {
  position: relative;
  cursor: default;
  padding: 0px;
  border: 0;
}
.dropdown-content {
  display: none;
  border: 1px solid var(--main-group-name-color);
  position: absolute;
  left: 30%;
  background-color: #FFF;
  border-radius: 10px;
  min-width: 160px;
  font-size: 80%;
  box-shadow: 0px 8px 16px 0px rgba(0,0,0,0.25);
  z-index: 1;
  padding: 0.1em;
  cursor: default;
}
.dropdown-content a {
  color: black;
  padding: 0.2em 1em;
  text-decoration: none;
  display: block;
}
.dropdown-content a:hover {background-color: #ddd;}
.dropdown:hover .dropdown-content {display: block;}

:root {
    --main-group-header-color: #DFDFDF;

    --main-group-name-color: #ff5959;
    //--main-group-header-color: #eb9d9d;

    /* crab */
    --main-group-name-color: #ecb1a1;
    //--main-group-header-color: #dbccc8;

    /* blue */
    --main-group-name-color: #90b4d2;
    //--main-group-header-color: #dae6f0;

    /* rpeat-default */
    --main-group-name-color: #F76D1C;
    --main-group-name-color: #FFC551;
    //--main-group-header-color: #FFC551;

    // dark theme
    --background-color: #2A2A2A;
    --group-name-color: white;
    --row-background-color-odd: #4A4A4A;
    --row-color-odd: white;
    --row-background-color-even: #777777;
    --row-color-even: white;
    --controls-background-color: #252525;

    // light theme
    --servername-color: black;
    --background-color: white;
    --group-name-color: #777;
    --row-background-color-odd: #EFEFEF;
    --row-color-odd: black;
    --row-background-color-even: white;
    --row-color-even: black;
    --controls-background-color: initial;
}

button.server-button, button.server-connected, button.server-disconnected, button.server-reconnect {
    border-radius: 5px;
    border: 1px;
    border-color: white;
    background-color: #CCC;
    color: #666;
}
button.server-connected {
   margin-bottom: 3px;
   color: green;
   background-color: #00FF00;
}
button.server-disconnected {
   margin-bottom: 3px;
   color: white;
   background-color: #FF1100;
}
button.server-reconnect {
   margin-bottom: 3px;
   color: white;
   background-color: #FF1100;
}

#dashboard { padding-bottom: 100px; }
// table styling
table,th,td {
  padding: .1ch;
  text-align: center;
  font-family: sans-serif;
  max-width: 1800px;
  margin: auto;
  margin-bottom: .4em;
  border: 1px;
  border-radius: 3px;
}
table.job-info, table.history-table {
  width: 1125px;
  table-layout: fixed;
}
.history-table th {
  font-weight: normal;
  font-size: 80%;
}
table.jobsgroup {
  width: 1125px;
  overflow-x: scroll;
}
table.job-info, table.history-table {
  table-layout: fixed;
}
.job-info td {
  font-size: 80%;
  text-align: left;
  overflow-x: scroll;
}
.group th {
  font-weight: normal;
  text-align: left;
  padding: 10px 0px 0px 5px;
  border-bottom: var(--main-group-name-color) 2px solid;
  border-radius: 5px;
}
.group a {
  color: #777;
  color: var(--group-name-color);
}
.job td {
  /*text-align: left;*/
}

td.elapsed, td.calendar, td.prevelapsed, td.kstate, td.retryattempt, td.pid {
  text-align: center;
}
td.elapsed, th.elapsed {
  width: 8ch;
  font-size: 80%;
  opacity: 25%;
}
th.elapsed {
  opacity: 100%;
}

th.nextstart, th.started,
td.nextstart, td.started {
  width: 18ch;
  font-size: 80%;
  text-align: center;
}
td.started, th.started {
  display: none;
}
td.previous, th.previous {
  width: 20ch;
  text-align: center;
}
td.previous a.previous {
  width: 20ch;
  text-align: center;
}
td.previous, th.previous {
  display: none;
}
a.prevstart, a.prevstop, a.prevelapsed, a.prevstate {
  width: 30ch;
  padding: .2ch;
  text-align: left;
}

td.schedule, a.schedule {
  width: 15ch;
  font-size: 90%;
  text-align: center;
}
a.cronstart, a.cronend, a.cronrestart {
  width: 30ch;
  /*padding: 1ch;*/
  /*padding-left: 1.5ch;*/
  text-align: left;
}

td.details, th.details {
  width: 20px;
  text-align: center;
  vertical-align: middle;
}
a.details {
  text-decoration-line: underline;
  text-decoration-style: dotted;
  text-decoration-color: var(--main-group-name-color);
}
a.name, a.jobid, a.runid, a.pid, a.retryattempt, a.status, a.info {
  width: 45ch;
  //padding: .2ch;
  text-align: left;
}
a.dependency:hover  { background-color: #fff; }
td.name, th.name {
  width: 35ch;
}
td.status {
}
td.timezone, th.timezone {
  width: 20ch;
  font-size: 80%;
  text-align: center;
}
td.kstate, th.kstate {
  width: 20px;
  text-align: center;
}
.kstate span>img {
  display: inline-block;
  height: 100%;
  vertical-align: middle;
}
.history-trail {
  width: 205px;
  text-align: left;
}
.history-trail span {
  display: inline-block;
  height: 100%;
  vertical-align: middle;
  white-space: normal;
  text-align: left;
  margin: 0px 2px 0px 2px;
  padding: 0 0 0 0;
}
.history-trail span>img {
  display: inline-block;
  height: 100%;
  vertical-align: middle;
}
.history-trail span>div { padding-left:5px; }
// job view history
.history-row td {
   padding-left: 5px;
   padding-right: 5px;
   cursor: default;
}
.history-row:hover {
   color: orange;
   cursor: pointer;
}

td.runuuid {
   text-align:left;
   font-size: 80%;
   vertical-align: middle;
   font-family: courier, menlo, and consolas;
   opacity: 0.3;
}
a.display-logs {
   text-decoration: underline;
   cursor: pointer;
   opacity: 1.0;
}
div.log-container {}
div.history-log {}

div.job-logs {
  max-width: 1100px;
  margin: auto;
}
div.job-stdout, div.job-stderr {
  border: 1px solid #DDD;
  border-radius: 15px;
  overflow-x: scroll;
  overflow-y: scroll;
  max-width: 1100px;
  margin: auto;
  min-height: 12em;
  max-height: 12em;
  background-color: #EEE;
  color: #333;
  padding: 10px;
  margin-bottom: 20px;
}
tbody tr.job {
  background-color: var(--row-background-color-even);
  color: var(--row-color-even);
}
tbody tr:nth-child(odd):not(:first-child) {
  background: #EFEFEF;
  background-color: var(--row-background-color-odd);
  color: var(--row-color-odd);
}
.header th {
  color: #AAA;
  font-size: 80%;
  font-weight: normal;
}
button.inspect-button { border: none; height: 20px; width: 16px; background-image: url("/assets/inspect.png"); background-repeat: no-repeat; }
button.hold-button    { border: none; height: 20px; width: 28px; background-image: url("/assets/hold.png"); }
button.resume-button  { border: none; height: 20px; width: 28px; background-image: url("/assets/resume.png"); }
button.start-button   { border: none; height: 20px; width: 28px; background-image: url("/assets/start.png"); }
button.stop-button    { border: none; height: 20px; width: 28px; background-image: url("/assets/stop.png"); }
button.restart-button { border: none; height: 20px; width: 28px; background-image: url("/assets/restart.png"); }

th.controls {
  padding-left: 0px;
  padding-right: 0px;
}
td.controls-on, td.controls-off {
  width: 30px;
  background-color: var(--controls-background-color);
  // padding-left: 0px;
  // padding-right: 0px;
}
td.controls-on button {
  width: 28px;
  height: 20px;
  opacity: 90%;
  border: 0px;
  vertical-align: middle;
  /* background-color: #91b6d4; */
  transition: opacity 0.15s ease-in;
}
td.controls-off button {
  width: 28px;
  height: 20px;
  opacity: 15%;
  border: 0px;
  vertical-align: middle;
}
#nav {
  width: 1100px;
  margin: auto;
  display: flex;
}
#nav_right, #nav_left {
  width: 50%;
  float: left;
}
#servername {
  font-size: 25px;
  font-family: sans-serif;
  color: var(--servername-color);
}
#servername a {
  text-decoration: none;
  color: var(--servername-color);
}
#servertime {
  opacity: 75%;
  font-family:monospace;
  color: var(--servername-color);
}
@keyframes pulse {
  0%   { opacity:1; }
  50%  { opacity:0.7; }
  100% { opacity:1; }
}
@-o-keyframes pulse{
  0%   { opacity:1; }
  50%  { opacity:0.7; }
  100% { opacity:1; }
}
@-moz-keyframes pulse{
  0%   { opacity:1; }
  50%  { opacity:0.7; }
  100% { opacity:1; }
}
@-webkit-keyframes pulse{
  0%   { opacity:1; }
  50%  { opacity:0.7; }
  100% { opacity:1; }
}
.animate-pulse {
   -webkit-animation: pulse 2s infinite;
   -moz-animation: pulse 2s infinite;
   -o-animation: pulse 2s infinite;
    animation: pulse 2s infinite;
}
#poweredby {
  width: 128px;
  height: 16px;
  margin-bottom: 10px;
}
#top-banner {
  width: 1125px;
  margin: auto;
  margin-bottom: 10px;
  border-radius: 5px;
}
`
