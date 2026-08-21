package rpeat

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"time"
)

func CreateDemo() {

	fmt.Println()
	sconf := createServerConfig()
	fmt.Println()

	sjson, _ := json.MarshalIndent(sconf, "", "  ")
	configFile := sconf.Abs(sconf.ConfigFile)
	fmt.Printf("  ServerConfig json: %s\n", configFile)
	if exists, _ := FileExists(configFile); !exists {
		WriteJSON(sjson, configFile)
	}

	jobs := createJobs(sconf.Owner)
	j, _ := json.MarshalIndent(jobs, "", "  ")
	jobsFiles := sconf.Abs(sconf.JobsFiles[0])
	fmt.Printf("  JobSpec json: %s\n", jobsFiles)
	if exists, _ := FileExists(jobsFiles); !exists {
		WriteJSON(j, jobsFiles)
	}

	auth := createAuth(sconf.Owner)
	a, _ := json.MarshalIndent(auth, "", "  ")
	authFile := sconf.Abs(sconf.AuthFile)
	fmt.Printf("  Auth json: %s\n", authFile)
	if exists, _ := FileExists(authFile); !exists {
		WriteJSON(a, authFile)
	}

	directoryDetails :=
		`
Feel free to modify and experiment with the demo files, 
as they will not be overwritten by rpeat-server. If you want to
reset the demo, either delete its directory or rename it.

Enjoy!

`
	fmt.Printf("%s", directoryDetails)

}

func WriteJSON(b []byte, file string) error {
	err := ioutil.WriteFile(file, b, os.FileMode(0644))
	return err
}

func createDir(dir string) {
	createdOrExisting := "Using existing"
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			err := os.MkdirAll(dir, os.FileMode(0700))
			if err != nil {
				ServerLogger.Fatal("failed to create rpeat® directory", err)
			}
			createdOrExisting = "Created new"
		}
	}
	absdir, err := filepath.Abs(dir)
	if err != nil {
		ServerLogger.Println("unable to find absolute path to rpeat® dir", err)
	}
	fmt.Printf("%s demo directory: %s\n", createdOrExisting, absdir)
}

func createJobs(user string) []JobSpec {

	jobs := make([]JobSpec, 3)

	jobs[0].Name = "Hello World!"
	CronStart := []string{"* * * * *"}
	jobs[0].CronStart = &CronStart
	jobs[0].Group = []string{"Demo"}
	Env := EnvList{"SLEEP=10", "WORLD=not the whole world"}
	jobs[0].Env = &Env
	Cmd := "/bin/sh -c echo 'hello $WORLD $WORLD2' && sleep $SLEEP"
	jobs[0].Cmd = &Cmd
	jobs[0].User = &user
	alerts := AlertActions{}
	success := Alert{}
	failure := Alert{}
	to := "no-reply@example.com"
	bcc := "shhh@example.com"
	success.To = []*string{&to}
	alerts.OnSuccess = &success
	failure.To = []*string{&to}
	failure.BCC = []*string{&bcc}
	alerts.OnFailure = &failure
	jobs[0].AlertActions = &alerts

	jobs[1].Name = "Start at 30s after each min"
	CronStart1 := []string{"30 * * * * *"}
	jobs[1].CronStart = &CronStart1
	jobs[1].Group = []string{"Demo"}
	new_york := "America/New_York"
	jobs[1].Timezone = &new_york
	MaxDuration := "20s"
	jobs[1].MaxDuration = &MaxDuration
	Cmd1 := "/bin/sh -c echo 'running for 60s' && sleep 60"
	jobs[1].Cmd = &Cmd1
	jobs[1].User = &user

	jobs[2].Name = "Run after 30 job"
	CronStart2 := []string{"@depends"}
	jobs[2].CronStart = &CronStart2
	hong_kong := "Asia/Hong_Kong"
	jobs[2].Timezone = &hong_kong
	Cmd2 := "/bin/sh -c echo 'I depend on you!'"
	jobs[2].Cmd = &Cmd2
	jobs[2].User = &user
	dep := Dependency{}
	dep.Dependencies = map[string]string{"Hello World!": "success|end", "Start at 30s after each min": "success|end"}
	dep.Action = "start"
	dep.Condition = "all"
	dep.Delay = "3s"
	jobs[2].Dependency = []Dependency{dep}
	permissions := Permission(map[string][]string{"all": []string{user, "linus"}})
	jobs[2].Permissions = &permissions

	return jobs

}

func createAuth(user string) []map[string]string {

	rand.Seed(time.Now().UnixNano())

	auth := make([]map[string]string, 1)
	auth[0] = make(map[string]string)
	auth[0]["User"] = user
	auth[0]["Secret"] = "demo" //randomString(10)

	return auth

}

func createServerConfig() ServerConfig {

	var conf ServerConfig

	u, err := user.Current()
	if err != nil {
		panic(err)
	}
	conf.HOST = "localhost"
	conf.PORT = "4334"

	conf.HOME = fmt.Sprintf("%s/.rpeat", u.HomeDir)
	createDir(filepath.Join(conf.HOME, "demo"))

	conf.JobsFiles = []string{"demo/jobs.json"}
	conf.ConfigFile = "demo/config.json"
	conf.AuthFile = "demo/auth.json"
	conf.Https = false
	conf.Timezone = time.Local.String()
	conf.Owner = u.Username
	conf.Name = "DEMO" //conf.Owner
	conf.Admin = []string{conf.Owner}
	conf.Permissions = Permission{"info": []string{conf.Owner}, "restart": []string{conf.Owner}}

	return conf

}

func GetOutboundIP() net.IP {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		ServerLogger.Fatal(err)
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)

	return localAddr.IP
}

// Returns an int >= min, < max
func randomInt(min, max int) int {
	return min + rand.Intn(max-min)
}

// Generate a random string of A-Z chars with len = l
func randomString(n int) string {
	pwChars := "ABCDEFGHIJKLMNOPQRSTUVWZYZabcdefghijklmnopqrstuvwxyz0123456789!*"
	bytes := make([]byte, n)
	for i := 0; i < n; i++ {
		bytes[i] = pwChars[rand.Intn(len(pwChars))]
	}
	return string(bytes)
}
