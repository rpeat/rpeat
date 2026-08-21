/*
    Copyright rpeat, rpeat.io All rights reserved.
    rpeat is a USPTO registered trademark

  Dual Licensed and Distributed under:

     GNU AFFERO GENERAL PUBLIC LICENSE Version 3

       https://www.gnu.org/licenses/agpl-3.0-standalone.html

     rpeat Commercial License

	   jeff@rpeat.io


*/

package main

import (
	"flag"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"rpeat"
	"strings"
	"time"
)

func main() {

	// start
	var HOME, HOST, PORT, jobs, config, auth, timezone string
	var useHttps, clean, nostate, nohistory, demo bool
	var cert, key string

	usr, _ := user.Current()
	userHomeDir := usr.HomeDir

	startCmd := flag.NewFlagSet("start", flag.ExitOnError)
	// FIXME: this should be default for no subcommand
	startCmd.Usage = func() {
		fmt.Printf(`
rpeat-server start

rpeat® is designed as a drop-in replacement for the ubiquitous cron. What it provides
is a lightweight rpeat® server that consume "Jobs", which not only have a start
time, but myriad options such as permissioning, logging, alerts, retry, and more!

All tasks (a.k.a. jobs) are defined in one or more json/xml files, either passed as -jobs or files listed in JobsFiles field of
the configuration json file (which itself is specified via -config)
`)
		fmt.Printf("\n\n")
		fmt.Printf("Usage: rpeat-server start [options]>\n\n")
		startCmd.PrintDefaults()
		fmt.Printf("...\n")
	}

	HOME, ok := os.LookupEnv("RPEAT_HOME")
	if !ok {
		HOME = filepath.Join(userHomeDir, ".rpeat")
	}
	startCmd.StringVar(&HOME, "HOME", HOME, "home `directory` of rpeat® - either ~/.rpeat or defined by RPEAT_HOME")
	startCmd.BoolVar(&demo, "demo", false, "create demo config, jobs and auth file and run")
	startCmd.StringVar(&HOST, "host", "localhost", "`address` of rpeat® server instance")
	startCmd.StringVar(&PORT, "port", "4334", "`port` of rpeat® server instance")
	startCmd.BoolVar(&useHttps, "https", true, "serve via TLS/https. Requires SSL cert and key to be specified")
	startCmd.BoolVar(&clean, "clean", false, "ignore previous jobs state and history. Same as specifying both --nostate --noclean")
	startCmd.BoolVar(&nostate, "nostate", false, "ignore previous jobs state if any (false)")
	startCmd.BoolVar(&nohistory, "nohistory", false, "ignore previous jobs history if any (false)")
	startCmd.StringVar(&cert, "cert", "server.crt", "TLS `certificate file`")
	startCmd.StringVar(&key, "key", "server.key", "TLS `key file`")
	startCmd.StringVar(&jobs, "jobs", "", "comma seperated array of jobs files")
	startCmd.StringVar(&config, "config", "", "rpeat® `configuration file`")
	startCmd.StringVar(&auth, "auth", "", "rpeat® `auth file`")
	startCmd.StringVar(&timezone, "timezone", "", "timezone `name` of server (defaults to system tz)")

	// stop
	var home, port string
	stopCmd := flag.NewFlagSet("stop", flag.ExitOnError)
	stopCmd.StringVar(&home, "home", filepath.Join(userHomeDir, ".rpeat"), "home of running server process")
	stopCmd.StringVar(&port, "port", "", "port of running server process")

	// restart  TODO: is this needed now? Seems unused as we use args from start
	restartCmd := flag.NewFlagSet("restart", flag.ExitOnError)
	restartCmd.StringVar(&home, "home", filepath.Join(userHomeDir, ".rpeat"), "home of running server process")
	restartCmd.StringVar(&port, "port", "", "port of running server process")

	// reload - same as touch home/port.pid for file watcher - DOES NOT RELOAD CONFIG
	reloadCmd := flag.NewFlagSet("reload", flag.ExitOnError)
	reloadCmd.StringVar(&home, "home", filepath.Join(userHomeDir, ".rpeat"), "home of running server process")
	reloadCmd.StringVar(&port, "port", "", "port of running server process")

	// gen-api-key

	// dispatch on subcommands

	help := fmt.Sprintf(`
rpeat-server (Version: %s, Build Date: %s)

rpeat® is designed to be a self-contained replacement for the ubiquitous cron. The lightweight rpeat®
server consumes 'jobs' which are simple configurations in xml or json describing when to run what.
Multiple rpeat® instances may be run per user and per server.

Usage:

  rpeat-server COMMAND [options]



  COMMAND:

    start: start an rpeat® server instance

    stop: stop a running rpeat® server

    restart: restart (reload configs) without stopping

    status: server details 

    rpeat-server log
         view server logs

    rpeat-server gen-api-key -user -job -config -port
        generate unique api key for given user or job

    cert: generate a self-signed x.509 certificate for TLS

    version: rpeat® version

    license: additional license information


  Use rpeat-server COMMAND -h for details on each command.

  Copyright rpeat.io. All rights reserved. rpeat® is a USPTO Registered Trademark of Lemnica Corp.



  LICENSE: rpeat® software is dual licensed and distributed with either of two licences:

     GNU AFFERO GENERAL PUBLIC LICENSE Version 3

       https://www.gnu.org/licenses/agpl-3.0-standalone.html


     For users who are unable or unwilling to comply with the GPL-3 Afferno we offer a commercial
     option that replaces the license with commercial version. All enquiries should be directed to
     the email below. There are various pricing tiers available depending on the requirements and
     status of the user.

     rpeat® Commercial License

	   jeff@rpeat.io

`, rpeat.VERSION, rpeat.BUILDDATE)
	if len(os.Args) == 1 || os.Args[1] == "-h" || os.Args[1] == "help" {
		fmt.Println(help)
		os.Exit(2)
	}

	switch os.Args[1] {
	case "version":
		fmt.Printf("rpeat® version: %s (%s, %s)\n", rpeat.VERSION, rpeat.BUILDDATE, rpeat.GITCOMMIT)
		os.Exit(0)
	case "license":
		fmt.Println(rpeat.Licenses)
		os.Exit(0)
	case "restart":
		startCmd.Parse(os.Args[2:])
		if startCmd.NFlag() == 0 {
			startCmd.PrintDefaults()
			os.Exit(2)
		}
		rpeat.Init()
		rpeat.ShutdownServer(HOME, PORT)
		fallthrough
	case "start":
		var c rpeat.ServerConfig
		if !startCmd.Parsed() {
			startCmd.Parse(os.Args[2:])
		}
		if startCmd.NFlag() == 0 {
			startCmd.PrintDefaults()
			os.Exit(2)
		}
		rpeat.Init()
		if demo {
			rpeat.CreateDemo()
			config = filepath.Join(userHomeDir, ".rpeat", "demo", "config.json")
		}

		jobsfiles := strings.Split(jobs, ",")

		if config != "" {
			var err error
			c, err = rpeat.LoadServerConfig(config, true)
			if err != nil {
				fmt.Println("problem processing config file. Terminating")
				os.Exit(1)
			}
			// replace any configurations with passed variables
			flaggedArgs := rpeat.FlagArgs(startCmd)
			for _, flag := range flaggedArgs {
				switch flag {
				case "host":
					c.HOST = HOST
				case "port":
					c.PORT = PORT
				case "https":
					c.Https = useHttps
				case "clean":
					c.Clean = true
					c.KeepHistory = false
				case "nostate":
					c.Clean = nostate
				case "nohistory":
					c.KeepHistory = !nohistory
				case "jobs":
					c.JobsFiles = jobsfiles
				case "cert":
					c.TLS.Cert, _ = filepath.Abs(cert)
				case "key":
					c.TLS.Key, _ = filepath.Abs(key)
				case "auth":
					c.AuthFile, _ = filepath.Abs(auth)
				case "timezone":
					c.Timezone = timezone
				case "HOME":
					c.HOME, _ = filepath.Abs(HOME)
				}
			}
		}
		c.GITCOMMIT = rpeat.GITCOMMIT
		c.BUILDDATE = rpeat.BUILDDATE
		c.VERSION = rpeat.VERSION

		for i, jobsfile := range c.JobsFiles {
			if exists, err := rpeat.FileExists(jobsfile); !exists {
				rpeat.ServerLogger.Fatal(fmt.Sprintf("problem reading jobs file %s: [%s]", jobsfile, err))
			}
			c.JobsFiles[i], _ = filepath.Abs(jobsfile)
		}

		rpeat.Init()
		rpeat.StartServer(c)
	case "stop":
		stopCmd.Parse(os.Args[2:])
		if stopCmd.NFlag() == 0 {
			stopCmd.PrintDefaults()
			os.Exit(2)
		}
		rpeat.Init()
		rpeat.ShutdownServer(home, port)
	case "reload":
		reloadCmd.Parse(os.Args[2:])
		if reloadCmd.NFlag() == 0 {
			reloadCmd.PrintDefaults()
			os.Exit(2)
		}
		currentTime := time.Now().Local()
		pidFile := filepath.Join(userHomeDir, ".rpeat", fmt.Sprintf("rpeat-%s", port))
		err := os.Chtimes(pidFile, currentTime, currentTime)
		if err != nil {
			fmt.Println(err)
		}
	default:
		fmt.Println(help)
		os.Exit(2)
	}

}
