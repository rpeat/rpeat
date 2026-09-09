package rpeat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Endpoint struct {
	URI string
	Key string
}

// Alert actions conditioned on rpeat job state. Zero or more may be
// specified depending on user requirements.
type AlertActions struct {
	// Alert based on associated state change
	OnSuccess     *Alert `json:"OnSuccess,omitempty" xml:"OnSuccess,omitempty"`
	OnFailure     *Alert `json:"OnFailure,omitempty" xml:"OnFailure,omitempty"`
	OnStopped     *Alert `json:"OnStopped,omitempty" xml:"OnStopped,omitempty"`
	OnEnd         *Alert `json:"OnEnd,omitempty" xml:"OnEnd,omitempty"`
	OnRestart     *Alert `json:"OnRestart,omitempty" xml:"OnRestart,omitempty"`
	OnRetrying    *Alert `json:"OnRetrying,omitempty" xml:"OnRetrying,omitempty"`
	OnRetryFailed *Alert `json:"OnRetryFailed,omitempty" xml:"OnRetryFailed,omitempty"`
	OnHold        *Alert `json:"OnHold,omitempty" xml:"OnHold,omitempty"`
	OnWarning     *Alert `json:"OnWarning,omitempty" xml:"OnWarning,omitempty"`
	OnDepFailed   *Alert `json:"OnDepFailed,omitempty" xml:"OnDepFailed,omitempty"`
	OnDepWarning  *Alert `json:"OnDepWarning,omitempty" xml:"OnDepWarning,omitempty"`
	OnChange      *Alert `json:"OnChange,omitempty" xml:"OnChange,omitempty"`

	// Details for alert server (see AlertParams).
	// If missing, the default Type and Endpoint correspond to
	// rpeat.Alert API call.
	//
	// Type support any of:
	//
	//   "rpeatio" - use rpeat.io alerts (default)
	//   "smtp"	- use Simple Mail Transfer Protocol (requires a server)
	//   "gmail" - use gmail account with access credentials set up
	//   "office365" - use Microsoft 365® (formerly Office)
	Type     *string `json:"Type,omitempty" xml:"Type,omitempty"`
	Endpoint *string `json:"Endpoint,omitempty" xml:"Endpoint,omitempty"`

	// Update fields in inherited Alerts if new Alerts is defined in Job
	Update *bool `json:"Update,omitempty" xml:"Update,omitempty"`

	// Disable rpeat.io Alerts dashboard
	NoRpeatio *bool `json:"NoRpeatio,omitempty" xml:"NoRpeatio,omitempty"`

	MaxLogLines *int `json:"MaxLogLines,omitempty" xml:"MaxLogLines,omitempty"`
}

func (job *Job) HasAlerts() bool {
	actions := job.AlertActions
	if actions.OnSuccess == nil &&
		actions.OnFailure == nil &&
		actions.OnStopped == nil &&
		actions.OnEnd == nil &&
		actions.OnRestart == nil &&
		actions.OnRetrying == nil &&
		actions.OnRetryFailed == nil &&
		actions.OnHold == nil &&
		actions.OnWarning == nil &&
		actions.OnDepFailed == nil &&
		actions.OnDepWarning == nil &&
		actions.OnChange == nil {
		return false
	}
	return true
}

// Alert contains fields describing the alert destination as well as
// optional job-specific details.
//
// For alerts sent to rpeat.Alerts, all fields are optional.
//
// If To, Cc, and Bcc must be a json array containing zero or
// more valid addresses. From will be replaced with an account
// specific address.  All addresses must be current accounts on
// rpeat.io or will be removed from the distribution.
type Alert struct {
	// For rpeat.Alerts, if To is empty, the messages will be
	// posted by not emailed.
	To []*string `json:"To,omitempty" xml:"To,omitempty"`

	// Cc and Bcc are optional and apply only to email
	CC  []*string `json:"CC,omitempty" xml:"CC,omitempty"`
	BCC []*string `json:"BCC,omitempty" xml:"BCC,omitempty"`

	// From is replaced internally with an rpeat.Alerts from
	// that is account specific
	From *string `json:"From,omitempty" xml:"From,omitempty"`

	Subject *string `json:"Subject,omitempty" xml:"Subject,omitempty"`
	Message *string `json:"Message,omitempty" xml:"Message,omitempty"`

	// Optional priority for alert escalation - currently unused
	Priority int `json:"Priority,omitempty" xml:"Priority,omitempty"`

	// Ability to override AlertActions destination
	Type     *string `json:"Type,omitempty" xml:"Type,omitempty"`
	Endpoint *string `json:"Endpoint,omitempty" xml:"Endpoint,omitempty"`

	// API key
	ApiKey string `json:"ApiKey,omitempty" xml:"ApiKey,omitempty"`
}

// Alert parameters sent as text/json to Endpoint specified, based on state change
type AlertParams struct {
	Name           string
	Group          string
	Tags           string
	CmdEval        string
	JobUUID        string
	RunUUID        string
	CronStart      string
	CronEnd        string
	CronRestart    string
	Elapsed        string
	NextStart      string
	NextStartUNIX  int64
	Started        string
	StartedUNIX    int64
	ExitCode       int
	PrevStop       string
	Timezone       string
	JobStateString string
	Retry          int
	RetryAttempt   int
	ServerName     string
	ServerKey      string `json:"ServerKey,omitempty"`
	StdOut         string
	StdOutFile     string
	StdErr         string
	StdErrFile     string
	History        []string
	// Permissions (users with view access, log access)
	Type     string
	Rpeatio  bool
	Endpoint string `json:"Endpoint,omitempty"`
	ApiKey   string `json:"ApiKey,omitempty"`
	Alert    Alert

	// flag to handle case where event occurs but should not alert
	send bool
}

func (job *Job) GetAlertParams() AlertParams {
	return job.getAlertParams()
}
func (job *Job) getAlertParams() AlertParams {

	job.RLock()
	defer job.RUnlock()

	maxLogLines := 20
	if job.AlertActions.MaxLogLines != nil {
		maxLogLines = *job.AlertActions.MaxLogLines
	}
	hist := make([]string, len(job.History))
	for i, h := range job.History {
		hist[i] = h.JobStateAbb
	}
	params := AlertParams{
		Name:           job.Name,
		Group:          Stringify(job.Group),
		Tags:           Stringify(job.Tags),
		CmdEval:        job.CmdEval,
		JobUUID:        job.JobUUID.String(),
		RunUUID:        job.RunUUID.String(),
		Elapsed:        job.Elapsed,
		NextStart:      job.NextStart,
		NextStartUNIX:  job.NextStartUNIX,
		Started:        job.Started,
		StartedUNIX:    job.StartedUNIX,
		ExitCode:       job.ExitCode,
		PrevStop:       job.PrevStop,
		Timezone:       job.Timezone,
		JobStateString: job.JobStateString,
		Retry:          job.Retry,
		RetryAttempt:   job.RetryAttempt,
		ServerName:     job.ServerName,
		ServerKey:      job.ServerKey,
		StdOut:         tailLog(job.Logging.stdoutFile, maxLogLines),
		StdOutFile:     job.Logging.stdoutFile,
		StdErr:         tailLog(job.Logging.stderrFile, maxLogLines),
		StdErrFile:     job.Logging.stderrFile,
		History:        hist,
	}
	if job.AlertActions.Type == nil {
		params.Type = "rpeat"
	} else {
		params.Type = *job.AlertActions.Type
	}
	if job.AlertActions.Endpoint == nil {
		params.Endpoint = "https://api-internal.rpeat.io/rpeat-alert"
	} else {
		params.Endpoint = *job.AlertActions.Endpoint
	}
	switch job.JobState {
	case JSuccess, JManualSuccess:
		if job.AlertActions.OnSuccess != nil {
			params.Alert = *job.AlertActions.OnSuccess
			params.send = true
		}
	case JFailed:
		if job.AlertActions.OnFailure != nil {
			params.Alert = *job.AlertActions.OnFailure
			params.send = true
		}
	case JStopped:
		if job.AlertActions.OnStopped != nil {
			params.Alert = *job.AlertActions.OnStopped
			params.send = true
		}
	case JEnd:
		if job.AlertActions.OnEnd != nil {
			params.Alert = *job.AlertActions.OnEnd
			params.send = true
		}
	case JRestart:
		if job.AlertActions.OnRestart != nil {
			params.Alert = *job.AlertActions.OnRestart
			params.send = true
		}
	case JRetrying:
		if job.AlertActions.OnRetrying != nil {
			params.Alert = *job.AlertActions.OnRetrying
			params.send = true
		}
	case JRetryFailed:
		if job.AlertActions.OnRetryFailed != nil {
			params.Alert = *job.AlertActions.OnRetryFailed
			params.send = true
		}
	case JHold:
		if job.AlertActions.OnHold != nil {
			params.Alert = *job.AlertActions.OnHold
			params.send = true
		}
	case JWarning:
		if job.AlertActions.OnWarning != nil {
			params.Alert = *job.AlertActions.OnWarning
			params.send = true
		}
	case JDepWarning:
		if job.AlertActions.OnDepWarning != nil {
			params.Alert = *job.AlertActions.OnDepWarning
			params.send = true
		}
	case JDepFailed:
		if job.AlertActions.OnDepFailed != nil {
			params.Alert = *job.AlertActions.OnDepFailed
			params.send = true
		}
	default:
		if job.AlertActions.OnChange != nil {
			params.Alert = *job.AlertActions.OnChange
			params.send = true
		}
	}
	return params
}

func (job *Job) sendAlert() {
	ServerLogger.Printf("Alerts JobState: %s", job.JobState)
	p := job.getAlertParams()
	if !p.send {
		ServerLogger.Printf("No Alert Required: %s", job.Name)
		return
	}

	// TODO: switch logic for handling different Type (e.g. redis, api, disk)
	switch p.Type {
	case "rpeat":
		rpeatioAlert(p)
	case "smtp":
		smtpAlert(p)
	case "gmail":
		gmailAlert(p)
	case "office365":
		office365Alert(p)
	case "json":
		jsonAlert(p)
	case "html":
		htmlAlert(p)
	case "plaintext", "text":
		plaintextAlert(p)
	case "markdown":
		markdownAlert(p)
	case "discord":
		discordAlert(p)
	case "slack":
		slackAlert(p)
	default:
		ServerLogger.Printf("Unsupported Alert Type: %s", p.Type)
	}

	if p.Type != "rpeat" && p.Rpeatio {
		go rpeatioAlert(p)
	}

}

func rpeatioAlert(alert AlertParams) {
	ServerLogger.Printf("rpeat.Alerts")
	key, ok := getApiKey()
	if !ok {
		ServerLogger.Printf("RPEAT_API_KEY environment variable not set - rpeat.io alerts will not work")
		return // should be in gui?
	}
	alert.ApiKey = key
	j, err := json.Marshal(alert)
	if err != nil {
		ServerLogger.Println(err)
		return
	}
	client := &http.Client{}
	URL := alert.Endpoint
	req, err := http.NewRequest("POST", URL, bytes.NewBuffer(j))
	req.Header.Add("Authorization", "Bearer "+key)
	resp, err := client.Do(req)
	if err != nil {
		ConnectionLogger.Println("rpeat® alert error:", err)
		return
	}
	ConnectionLogger.Printf("rpeat® rpeat.Alert Server Status: %s", resp.Status)
	resp.Body.Close()
}

// template function to read local file for either
// attachment to alert or to use as (template) for
// alert message content.
func ReadFile()   {}
func AttachFile() {}

// ability to upload an artifact (file) to remote storage
func CopyFile() {}

// Heartbeat parameters
type HeartbeatParams struct {
	ServerName string `json:"ServerName,omitempty"`
	ServerKey  string `json:"ServerKey,omitempty"`
	ApiKey     string `json:"ApiKey"`
	Next       int64
}

func (server ServerConfig) rpeatioHeartbeat(hb HeartbeatParams) {

	j, err := json.Marshal(hb)
	if err != nil {
		ServerLogger.Println(err)
		return
	}

	client := &http.Client{}
	URL := server.ApiEndpoint + "/rpeat-heartbeat"
	req, err := http.NewRequest("POST", URL, bytes.NewBuffer(j))
	req.Header.Add("Authorization", "Bearer "+hb.ApiKey)
	resp, err := client.Do(req)
	if err != nil {
		ConnectionLogger.Println("rpeat® alert error:", err)
		return
	}
	ConnectionLogger.Printf("rpeat® rpeat.Alert Server Status: %s", resp.Status)
	resp.Body.Close()
}

var DefaultAlertMessageHTML = `
Name: {{ .Name }}<br/>
Status: {{ .JobStateString }}<br/>
<br/>
Elapsed: {{ .Elapsed }}<br/>
Started: {{ .Started }} {{ .Timezone }}<br/>
Ended: {{ .PrevStop }} {{ .Timezone }}<br/>
<hr/>
Cmd: {{ .CmdEval }}<br/>
StdOut:<br/>
<pre>
{{ .StdOut }}
</pre>
<br/>
StdErr:<br/>
<pre>
{{ .StdErr }}
</pre>
<br/>
<hr/>
JobUUID: {{ .JobUUID }}<br/>
RunUUID: {{ .RunUUID }}<br/>
<br/>
<img src="https://rpeat.io/assets/img/poweredbyrpeat.png"/>
`
var DefaultAlertMessageMarkdown = `
**Server**: rpeat-{{ .ServerName }}
**Name:** {{ .Name }}
**Status:** {{ .JobStateString }}

**Elapsed:** {{ .Elapsed }}
**Started:** {{ .Started }} {{ .Timezone }}
**Ended:** {{ .PrevStop }} {{ .Timezone }}

**Cmd:** {{ .CmdEval }}

**Stdout:**
` + "```" + `
{{ .StdOut }}
` + "```" + `

**Stderr:**
` + "```" + `
{{ .StdErr }}
` + "```" + `
--
**JobUUID:** {{ .JobUUID }}
**RunUUID:** {{ .RunUUID }}

`

var DefaultAlertMessagePlainText = `
Server: rpeat-{{ .ServerName }}
Name: {{ .Name }}
Status: {{ .JobStateString }}

Elapsed: {{ .Elapsed }}
Started: {{ .Started }} {{ .Timezone }}
Ended: {{ .PrevStop }} {{ .Timezone }}

Cmd: {{ .CmdEval }}
--
JobUUID: {{ .JobUUID }}
RunUUID: {{ .RunUUID }}
`

// Integrations

func teamsAlert(alert AlertParams) error {
	return nil
}
func telegramAlert(alert AlertParams) error {
	return nil
}

func jsonAlert(alert AlertParams) error {
	j, err := json.Marshal(alert)
	if err != nil {
		ServerLogger.Println(err)
		return err
	}
	req, err := http.NewRequest("POST", alert.Endpoint, bytes.NewBuffer(j))
	if err != nil {
		ServerLogger.Println("failed json alert:", err)
		return err
	}
	req.Header.Set("Content-Type", "text/json") // Set headers

	client := &http.Client{Timeout: 10 * time.Second} // Set timeout
	resp, err := client.Do(req)
	if err != nil {
		ServerLogger.Println(err)
		return err
	}
	defer resp.Body.Close()

	return nil
}
func plaintextAlert(alert AlertParams) error {
	var msgBuf bytes.Buffer
	tmpl, _ := template.New("Msg").Parse(DefaultAlertMessagePlainText)
	alert.CmdEval = strconv.Quote(alert.CmdEval)
	tmpl.Execute(&msgBuf, alert)
	msgBuf = *(bytes.NewBuffer(replaceHTML(msgBuf.Bytes())))

	req, _ := http.NewRequest("POST", alert.Endpoint, &msgBuf)
	req.Header.Set("Content-Type", "text/html; charset=utf-8") // Set headers

	client := &http.Client{Timeout: 10 * time.Second} // Set timeout
	resp, err := client.Do(req)
	if err != nil {
		ServerLogger.Println(err)
		return err
	}
	defer resp.Body.Close()

	return nil
}
func htmlAlert(alert AlertParams) error {
	var msgBuf bytes.Buffer
	tmpl, _ := template.New("Msg").Parse("Server: {{ .ServerName }}<br/>" + DefaultAlertMessageHTML)
	alert.CmdEval = strconv.Quote(alert.CmdEval)
	tmpl.Execute(&msgBuf, alert)
	msgBuf = *(bytes.NewBuffer(replaceHTML(msgBuf.Bytes())))

	req, _ := http.NewRequest("POST", alert.Endpoint, &msgBuf)
	req.Header.Set("Content-Type", "text/html; charset=utf-8") // Set headers

	client := &http.Client{Timeout: 10 * time.Second} // Set timeout
	resp, err := client.Do(req)
	if err != nil {
		ServerLogger.Println(err)
		return err
	}
	defer resp.Body.Close()

	return nil
}
func markdownAlert(alert AlertParams) error {
	var msgBuf bytes.Buffer
	tmpl, _ := template.New("Msg").Parse(DefaultAlertMessageMarkdown)
	alert.CmdEval = strconv.Quote(alert.CmdEval)
	tmpl.Execute(&msgBuf, alert)
	msgBuf = *(bytes.NewBuffer(replaceHTML(msgBuf.Bytes())))

	req, _ := http.NewRequest("POST", alert.Endpoint, &msgBuf)
	req.Header.Set("Content-Type", "text/markdown") // Set headers

	client := &http.Client{Timeout: 10 * time.Second} // Set timeout
	resp, err := client.Do(req)
	if err != nil {
		ServerLogger.Println(err)
		return err
	}
	defer resp.Body.Close()

	return nil
}

func discordAlert(alert AlertParams) error {
	var DefaultAlertMessageDiscord = `
{
  "content": "%s",
  "username": "%s",
  "avatar_url": "https://rpeat.io/assets/img/rpeat-icon.png",
  "embeds": [
    {
      "title": "{{ .Name }}",
      "description": "*{{ .JobStateString }}*",
      "color": 65280,
      "fields": [
        {
          "name": "Cmd",
          "value": {{ .CmdEval }},
          "inline": false
        },
        {
          "name": "Elapsed",
          "value": "{{ .Elapsed }}",
          "inline": true
        },
        {
          "name": "ExitCode",
          "value": "{{ .ExitCode }}",
          "inline": true
        },
        {
          "name": "",
          "value": "",
          "inline": true
        },
        {
          "name": "Started",
          "value": "{{ .Started }}",
          "inline": true
        },
        {
          "name": "Ended",
          "value": "{{ .PrevStop }}",
          "inline": true
        },
        {
          "name": "Timezone",
          "value": "{{ .Timezone }}",
          "inline": true
        },
        {
          "name": "JobUUID",
          "value": "{{ .JobUUID }}",
          "inline": false
        },
        {
          "name": "RunUUID",
          "value": "{{ .RunUUID }}",
          "inline": false
        }
      ],
      "footer": {
        "text": "Powered by rpeat®"
      }
    }
  ]
}
`
	username := fmt.Sprintf("rpeat-%s", alert.ServerName)
	if alert.Alert.From != nil {
		username = fmt.Sprintf("%s", *alert.Alert.From)
	}
	content := ""
	if alert.Alert.To != nil {
		var tos []string
		for _, to := range alert.Alert.To {
			if to != nil {
				tos = append(tos, *to)
			}
		}
		content = fmt.Sprintf("<%s>", strings.Join(tos, "> <"))
	}
	DefaultAlertMessageDiscord = fmt.Sprintf(DefaultAlertMessageDiscord, content, username)

	var msgBuf bytes.Buffer
	tmpl, _ := template.New("Msg").Parse(DefaultAlertMessageDiscord)
	alert.CmdEval = strconv.Quote(alert.CmdEval)
	tmpl.Execute(&msgBuf, alert)

	msgBuf = *(bytes.NewBuffer(replaceHTML(msgBuf.Bytes())))

	ServerLogger.Printf("Sending Endpoint: %s", alert.Endpoint)

	req, _ := http.NewRequest("POST", alert.Endpoint, &msgBuf)
	req.Header.Set("Content-Type", "application/json") // Set headers

	client := &http.Client{Timeout: 10 * time.Second} // Set timeout
	resp, err := client.Do(req)
	if err != nil {
		ServerLogger.Println(err)
		return err
	}
	defer resp.Body.Close()

	return nil
}

func slackAlert(alert AlertParams) error {
	var DefaultAlertMessageSlack = `
{
	"blocks": [
		{
			"type": "card",
            "icon": {
				"type": "image",
				"image_url": "https://rpeat.io/assets/img/rpeat-icon-34x36.png",
				"alt_text": "rpeat icon"
			},
			"title": {
				"type": "mrkdwn",
				"text": "{{ .Name }}",
				"verbatim": false
			},
			"subtitle": {
				"type": "mrkdwn",
				"text": "{{ .JobStateString }}",
				"verbatim": false
			},
			"body": {
				"type": "mrkdwn",
				"text": "*Cmd:*   {{ .CmdEval }}",
				"verbatim": false
			}
		},
        {
			"type": "section",
            "fields": [
				{
					"type": "mrkdwn",
					"text": "*Elapsed:* {{ .Elapsed }}"
				},
				{
					"type": "mrkdwn",
					"text": "*ExitCode:* {{ .ExitCode }}"
				},
				{
					"type": "mrkdwn",
					"text": "*Started:* {{ .Started }}"
				},
				{
					"type": "mrkdwn",
					"text": "*Ended:* {{ .PrevStop }}"
				},
				{
					"type": "mrkdwn",
					"text": "*Timezone:* {{ .Timezone }}"
				},
				{
					"type": "mrkdwn",
					"text": " "
				},
				{
					"type": "mrkdwn",
					"text": "*JobUUID:* {{ .JobUUID }}"
				},
				{
					"type": "mrkdwn",
					"text": "*RunUUID:* {{ .JobUUID }}"
				}
            ]
        },
		{
			"type": "context",
			"elements": [
				{
					"type": "mrkdwn",
					"text": "rpeat-{{ .ServerName }} | Powered by rpeat®"
				}
			]
		}
	]
}
`
	var msgBuf bytes.Buffer
	tmpl, _ := template.New("Msg").Parse(DefaultAlertMessageSlack)
	alert.CmdEval = fixQuotes(alert.CmdEval)
	tmpl.Execute(&msgBuf, alert)
	msgBuf = *(bytes.NewBuffer(replaceHTML(msgBuf.Bytes())))

	req, _ := http.NewRequest("POST", alert.Endpoint, &msgBuf)
	req.Header.Set("Content-Type", "application/json") // Set headers

	client := &http.Client{Timeout: 10 * time.Second} // Set timeout
	resp, err := client.Do(req)
	ServerLogger.Println(resp)
	if err != nil {
		ServerLogger.Println(err)
		return err
	}
	defer resp.Body.Close()

	return nil
}
