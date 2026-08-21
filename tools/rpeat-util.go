package main

/*

rpeat-utils [CMD] [OPTIONS]

CMD:

  validate
    -h, -help
    -jobs       // comma-sep string of job files

  next
    -cron
    -cronfile
    -tz
    -cal
    -calendarDirs
    -reqcal
    -rollback
    -asof
    -sep
    -timefmt
    -n


*/

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"rpeat"
	"strings"
	"time"
)

func main() {
	rpeat.Init()

	usr, _ := user.Current()
	userHomeDir := usr.HomeDir

	help := fmt.Sprintf(`
rpeat-util  (%s)

utility tools for helping to create, manage, validate and view rpeat jobs

Usage:

  rpeat-util COMMAND [options]

  COMMAND:

    next: parse cron expressions using tz, calendars at arbitrary moment in time

      Process single expression or expression file using optionally specified 
      timezone, calendar and calendarDirs expected in JobSpec. Special 'asof'
      argument allows for next scheduled trigger to be tested at arbitrary moments in
      time.  Output is delimited suitable for further inspection

    date: date variable expansion as used in DateEnv

      Convert specially formatted string into formatted date variable, similar
      to UNIX date function.

         rpeat-util date -datevar DATEVAR[,ADJ[,CAL]]

      If CAL is provided, -calendarDirs must contain specified calendar 

      e.g.

         # today plus two days
         rpeat-util date -datevar CCYYMMDD,+2D

         # today minus two months, using MF only
         rpeat-util date -datevar CCYYMMDD,+2M,MF -calendarDirs=caldir

    convert: convert job file between xml and json format

        -f format all [xml |json]

    validate: comprehensive validation check on list of job file(s)

      Validate checks expression, environment variables, dependencies, timezone
      calendars, permissions and templates for any syntax issues or missingness
 
        -s short format

      Restrict checks to 
        -j job uuid or pattern
        -p pattern
        -t tags
        -g group
      
      return value 0: ok, 1: warning, 2: error

    jobstate: conversion tool to investigate binary .rj files containing job state

  Use rpeat-util COMMAND -h for additional details on each command.
  
  Copyright rpeat.io. All rights reserved. rpeat® is a USPTO registered trademark of Lemnica Corp.

  Dual Licensed:

     GNU AFFERO GENERAL PUBLIC LICENSE Version 3

       https://www.gnu.org/licenses/agpl-3.0-standalone.html

     rpeat® Commercial License

	   jeff@rpeat.io

  
`, rpeat.BUILDDATE)
	// next
	var cron, cronfile, tz, cal, calendarDirs, asof, timefmt, sep string
	var reqcal, rollback, header, endof, verbose, asjson, asxml bool
	var jitter int
	var N int
	nextCmd := flag.NewFlagSet("next", flag.ExitOnError)
	nextCmd.StringVar(&cron, "cron", "", "required")
	nextCmd.StringVar(&cronfile, "cronfile", "", "file containing one cron spec per lin")
	nextCmd.StringVar(&tz, "tz", "", "timezone (empty)")
	nextCmd.StringVar(&cal, "cal", "", "calendar (empty)")
	nextCmd.StringVar(&calendarDirs, "calendarDirs", "", "calendar directories (empty)")
	nextCmd.BoolVar(&reqcal, "reqcal", false, "if calendar unavailable for period return error, otherwise fallback to cron-only")
	nextCmd.BoolVar(&rollback, "rollback", false, "if calendar date is not available for cron-interval, rollback to prior calendar date. Defaults to next.")
	nextCmd.BoolVar(&endof, "endof", false, "sets EndOf flag in cron")
	nextCmd.IntVar(&jitter, "jitter", 0, "add max `seconds` of random time (aka jitter) to start time")
	nextCmd.StringVar(&asof, "asof", "", "asof time to calculate next start time formatted as YYYYMMDDmmddss (current time)")
	nextCmd.StringVar(&sep, "sep", "|", "output field seperator (|)")
	nextCmd.StringVar(&timefmt, "timefmt", "2006-01-02T15:04:05", "time format string expressed as golang format string (2006-01-02T15:04:05)")
	nextCmd.BoolVar(&header, "header", true, "include column name header")
	nextCmd.IntVar(&N, "n", 1, "number of future times to display")

	// validate
	var jobfiles, configFile, authFile string // TODO: add configFile and home option
	validateCmd := flag.NewFlagSet("validate", flag.ExitOnError)
	validateCmd.BoolVar(&asjson, "json", false, "return json object only")
	validateCmd.StringVar(&jobfiles, "jobfiles", "", "comma seperated list of job `files`")
	validateCmd.StringVar(&authFile, "authFile", "", "authentication `file` (used for user validations if specified)")
	validateCmd.StringVar(&configFile, "configFile", "", "configuration `file` (used for user validations if specified)")

	// convert json <-> xml
	convertCmd := flag.NewFlagSet("convert", flag.ExitOnError)
	convertCmd.BoolVar(&asjson, "json", false, "convert to json")
	convertCmd.BoolVar(&asxml, "xml", false, "convert to xml")

	// jobstate
	var rj string
	var uncompressed bool
	jobstateCmd := flag.NewFlagSet("jobstate", flag.ExitOnError)
	jobstateCmd.StringVar(&rj, "rj", "", ".rj file with job state")
	jobstateCmd.BoolVar(&uncompressed, "uncompressed", false, "is .rj file uncompressed (deprecated)")

	// date
	var datevar, timezone, caldirs string
	dateCmd := flag.NewFlagSet("date", flag.ExitOnError)
	dateCmd.StringVar(&datevar, "datevar", "", "one or more variables CC,YY,MM,hh,mm,ss, and/or QTR to be automagically replaced with two-digit century, year, month,\nhour, minute, second or quarter, respectively.  DATEVAR[[,[+-][0-9]{0,}[D|W|M|Q|Y][,CAL]]")
	dateCmd.StringVar(&timezone, "tz", "", "a valid IANA timezone. e.g. America/Chicago")
	dateCmd.StringVar(&caldirs, "calendarDirs", "", "comma sep string of one or more calendar directories")
	dateCmd.StringVar(&asof, "asof", "", "asof time to calculate next start time formatted as YYYYMMDDmmddss (current time)")

	// gen-tls-cert
	var dir, host string
	certCmd := flag.NewFlagSet("cert", flag.ExitOnError)
	certCmd.StringVar(&dir, "dir", filepath.Join(userHomeDir, ".rpeat"), "install `directory`")
	certCmd.StringVar(&host, "host", "", "domain name or ip `address`")

	if len(os.Args) == 1 || os.Args[1] == "-h" || os.Args[1] == "help" {
		fmt.Println(help)
		os.Exit(2)
	}

	switch os.Args[1] {
	case "next":
		nextCmd.Parse(os.Args[2:])
		if nextCmd.NFlag() == 0 {
			nextCmd.PrintDefaults()
			os.Exit(2)
		}
		var t time.Time
		var err error
		loc, _ := time.LoadLocation(tz)
		if asof == "" {
			t = time.Now().In(loc)
			//log.Printf("using current time %s\n", t.String())
			asof = t.Format("20060102150405")
		} else {
			t, err = time.ParseInLocation("20060102150405", asof, loc)
			if err != nil {
				panic(err)
			}
			os.Setenv("RPEAT_NOW", asof)
		}

		var crons []string
		if cron != "" {
			crons = append(crons, cron)
		}
		if cronfile != "" {
			file, err := os.Open(cronfile)
			if err != nil {
				panic(err)
			}
			defer file.Close()
			scanner := bufio.NewScanner(file)
			for scanner.Scan() {
				crons = append(crons, scanner.Text())
			}
		}

		asof = t.Format(timefmt)
		if err != nil {
			log.Fatal(fmt.Sprintf("tz: %s failed to parse", tz))
		}
		if header {
			fmt.Println(strings.Join([]string{"cron", "timezone", "calendar", "asOf", "triggerDate", "humanDate", "duration"}, sep))
		}
		//var asofstring string
		for ci := range crons {
			c, err := rpeat.ParseCron(crons[ci], tz, cal, []string{calendarDirs}, rollback, reqcal, jitter)
			c.EndOf = endof
			for i := 0; i < N; i++ {
				//asofstring = setTime(asof, timefmt, loc)

				if err != nil {
					panic(err)
				}
				d, next, err := c.NextStart("")

				if err != nil {
					log.Println(err)
				}
				if next.IsZero() {
					fmt.Println(strings.Join([]string{crons[ci], tz, cal, asof, "NA", "NA", "NA"}, sep))
					//fmt.Println(strings.Join([]string{cron,tz,cal,asofstring,"NA","NA","NA"}, sep))
				} else {
					nxtfmt := next.In(loc).Format(timefmt)
					nxt := next.Format(time.ANSIC)
					dur := rpeat.DHMS(d)
					fmt.Println(strings.Join([]string{crons[ci], tz, cal, asof, nxtfmt, nxt, dur}, sep))
					//fmt.Println(strings.Join([]string{cron,tz,cal,asofstring,nxtfmt,nxt,dur},sep))
				}

				newAsOf := next //.Add(time.Second).In(loc)
				asof = newAsOf.Format(timefmt)
				os.Setenv("RPEAT_NOW", newAsOf.Format("20060102150405"))
				//asof = next.In(loc).Format("20060102150405")
			}
		}
	case "validate":
		validateCmd.Parse(os.Args[2:])
		if validateCmd.NArg() == 0 {
			validateCmd.PrintDefaults()
			os.Exit(2)
		}
		//jobs := strings.Split(jobfiles, ",")
		jobs := validateCmd.Args()
		if configFile != "" {
			serverConf, err := rpeat.LoadServerConfig(configFile, false)
			if err != nil {
				fmt.Println("unable to load configuration file - aborting")
				os.Exit(1)
			}
			jobs = serverConf.JobsFiles
			authFile = serverConf.AuthFile
			if serverConf.ApiKey == "" {
				fmt.Println("no ApiKey or RPEAT_API_TOKEN set")
			}
			// TODO: check apiKey
		}

		verbose = true
		if asjson {
			verbose = false
		}
		exceptions, all_jve := rpeat.ValidateJobs(jobs, configFile, authFile, verbose)
		if !exceptions.Validated() {
			fmt.Println("unable to validate jobs - aborting")
			os.Exit(1)
		}
		if asjson {
			j, _ := json.Marshal(all_jve)
			fmt.Println(string(j))
		}
		if exceptions.HasError() {
			os.Exit(2)
		}
		if exceptions.HasWarning() {
			os.Exit(1)
		}
		if exceptions.JState == rpeat.JConfigError {
			os.Exit(2)
		}
	case "convert":
		convertCmd.Usage = func() {
			fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s [options] <job-file> [... <job-file>]\n", os.Args[0])
			fmt.Fprintln(flag.CommandLine.Output(), "\nOptions:\n")
			convertCmd.PrintDefaults()
			fmt.Fprintln(flag.CommandLine.Output(), "\nPositional arguments:\n")
			fmt.Fprintln(flag.CommandLine.Output(), "  <job-file> job files to convert \n")
		}
		convertCmd.Parse(os.Args[2:])
		if convertCmd.NArg() == 0 || os.Args[1] == "-h" {
			convertCmd.Usage()
			os.Exit(2)
		}
		jobs := convertCmd.Args()
		for _, jobfile := range jobs {
			rpeat.ConvertJobsFile(jobfile, asjson, asxml)
		}
	case "jobstate":
		jobstateCmd.Parse(os.Args[2:])
		if jobstateCmd.NFlag() == 0 {
			jobstateCmd.PrintDefaults()
			os.Exit(2)
		}
		job, err := rpeat.LoadJobState(rj, !uncompressed)
		if err != nil {
			panic(err)
		}
		j, err := json.Marshal(job)
		if err != nil {
			panic(err)
		}
		fmt.Println(string(j))
	case "date":
		dateCmd.Parse(os.Args[2:])
		if dateCmd.NFlag() == 0 {
			dateCmd.PrintDefaults()
			os.Exit(2)
		}
		calendarDirs := strings.Split(caldirs, ",")
		dt, err := rpeat.ConvertDate(datevar, timezone, calendarDirs, asof)
		if err != nil {
			dateenverr := err.(rpeat.DateEnvError)
			fmt.Printf("%s: %s\n", dateenverr.Exception, dateenverr.Error())
			os.Exit(1)
		}
		fmt.Printf("%s => %s\n", datevar, dt)
	case "cert":
		certCmd.Parse(os.Args[2:])
		if certCmd.NFlag() == 0 {
			certCmd.PrintDefaults()
			os.Exit(2)
		}
		pemfiles, err := rpeat.CreateX509(host, dir, false)
		if err != nil {
			os.Exit(2)
		}
		fmt.Printf("\nSelf signed 'rpeat®' X.509 certificate and key created:\n  %s\n  %s\n\n", pemfiles.Cert, pemfiles.Key)
	default:
		flag.PrintDefaults()
	}
}
