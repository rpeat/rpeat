package rpeat

import (
	"bufio"
	"bytes"
	"fmt"
	"github.com/fsnotify/fsnotify"
	"io"
	"io/ioutil"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	InfoColor    = "\033[1;34m%s\033[0m"
	NoticeColor  = "\033[1;36m%s\033[0m"
	WarningColor = "\033[1;33m%s\033[0m"
	ErrorColor   = "\033[1;31m%s\033[0m"
	DebugColor   = "\033[0;36m%s\033[0m"

	Red    = "\033[0;31m%s\033[0m"
	Green  = "\033[0;32m%s\033[0m"
	Orange = "\033[0;38;5;208m%s\033[0m"
)

func FileExists(file string) (bool, error) {
	var err error
	if _, err = os.Stat(file); err == nil {
		return true, err
	}
	return false, err
}

// internal helper functions
func mergeMdays(d1 []int, d2 []int) []int {
	mapMdays := make(map[int]int)

	// loop over each and add to map
	for i := 0; i < len(d1); i++ {
		mapMdays[d1[i]] = d1[i]
	}
	for i := 0; i < len(d2); i++ {
		mapMdays[d2[i]] = d2[i]
	}

	// convert to array
	md := make([]int, 0, len(mapMdays))
	for k, _ := range mapMdays {
		md = append(md, k)
	}
	sort.Ints(md)
	return md
}

func makeMdaysValid(year int, mon int, mdays []int) []int {
	mapMdays := make(map[int]int)

	loc, _ := time.LoadLocation("UTC")
	var dt time.Time
	for d := 0; d < len(mdays); d++ {
		dt = time.Date(year, time.Month(mon), mdays[d], int(0), 0, 0, 0, loc)
		//log.Printf("[makeMdaysValid] dt:%s", dt.String())
		if dt.Month() == time.Month(mon) && dt.Day() == mdays[d] {
			mapMdays[mdays[d]] = mdays[d]
		}
	}
	//if len(mapMdays) == 0 {
	//    dt = dt.AddDate(0,0,-1)
	//    log.Printf("[makeMdaysValid] dt.AddDate(0,0,-1):%s dt.Day:%d", dt.String(), dt.Day())
	//    mapMdays[mdays[0]] = dt.Day()
	//}
	//log.Println(mapMdays)
	md := make([]int, 0, len(mapMdays))
	for k, _ := range mapMdays {
		md = append(md, k)
	}
	sort.Ints(md)
	return md
}

// generate unique array of future mday values for mon from current wday
func makeMdays(year int, mon int, wdays []int) []int {

	loc, _ := time.LoadLocation("UTC")

	var mdays []int
	for d := 1; d <= 31; d++ {
		dt := time.Date(year, time.Month(mon), d, int(0), int(0), int(0), int(0), loc)

		mday := int(dt.Day())
		wday := int(dt.Weekday())

		if mday < d {
			break
		}

		for _, wd := range wdays {
			if wd == wday { //&& mday == d {
				mdays = append(mdays, mday)
			}
		}
	}
	return mdays
}

func makerange(s int, len int, step int) []int {
	r := make([]int, len)
	truelen := 0
	for i := 0; i < len; i = i + step {
		truelen++
		r[truelen-1] = s + i
	}
	r = r[:truelen]
	return r
}

func (job *Job) ElapsedSeconds() int64 {
	var t int64
	if job.JobState == JRunning {
		t = elapsedToInt(time.Now().Sub(job.prevStart).Round(time.Second))
	} else {
		t = elapsedToInt(job.elapsed)
	}
	return t
}
func elapsedToInt(duration time.Duration) int64 {
	return int64(math.Trunc(duration.Seconds()))
}
func dhms(duration time.Duration) string {
	var d, h, m, s int64
	d = 0
	h = 0
	m = 0

	t := int64(math.Trunc(duration.Seconds()))

	if t >= 86400 {
		d = int64(math.Trunc(float64(t / 86400)))
	}
	if t >= 3600 {
		h = int64(math.Abs(float64(d)*24 - math.Trunc(float64(t/3600))))
	}
	if t >= 60 {
		m = int64(math.Trunc(float64(t/60)) - (math.Trunc(float64(t/3600)) * 60.0))
	}
	s = int64(t) % 60

	ctime := []int64{d, h, m, s}
	labels := []string{"d", "h", "m", "s"}
	var i int
	for i = 0; i < 3; i++ {
		if ctime[i] != 0 {
			break
		}
	}
	var time string
	if i == 3 {
		time = fmt.Sprintf("    %2d%s", ctime[i], labels[i])
	} else {
		time = fmt.Sprintf("%2d%s %2d%s", ctime[i], labels[i], ctime[i+1], labels[i+1])
	}
	return time
}

var DHMS = dhms

func stringInSlice(str string, slice []string) bool {
	for _, s := range slice {
		if str == s {
			return true
		}
	}
	return false
}

var StringInSlice = stringInSlice

func StringsInSlice(strs []string, slice []string, notIn bool) []string {
	var inSlice []string
	for _, s := range strs {
		isIn := stringInSlice(s, slice)
		if isIn && !notIn {
			inSlice = append(inSlice, s)
		}
		if !isIn && notIn {
			inSlice = append(inSlice, s)
		}
	}
	return inSlice
}

func createPidFile(home, port string, pid int) string {
	err := os.MkdirAll(home, os.FileMode(0770))
	if err != nil {
		ServerLogger.Fatal("can't create temporary rpeat directory in '" + home + "'")
	}
	spid := []byte(strconv.Itoa(pid))
	pidfile := filepath.Join(home, fmt.Sprintf("rpeat-%s", port))
	err = ioutil.WriteFile(pidfile, spid, 0644)
	if err != nil {
		ServerLogger.Fatal("unable to write pid file", pidfile)
	}
	return pidfile
}

func removePidFile(home, port string) {
	pidfile := filepath.Join(home, fmt.Sprintf("rpeat-%s", port))
	exists, err := FileExists(pidfile)
	if err != nil {
		ServerLogger.Fatal("error with process file", err)
	}
	if exists {
		err = os.Remove(pidfile)
		if err != nil {
			ServerLogger.Fatal(err)
		}
	}
}

func ShutdownServer(home, port string) {
	ServerLogger.Println("Shutdown command recieved")
	pidfile := filepath.Join(home, fmt.Sprintf("rpeat-%s", port))
	exists, err := FileExists(pidfile)
	if err != nil {
		ServerLogger.Fatal("error with process file", err)
	}
	if exists {
		bytes, err := ioutil.ReadFile(pidfile)
		if err != nil {
			ServerLogger.Fatal("error reading process file", err)
		}
		pid, _ := strconv.Atoi(string(bytes))
		ServerLogger.Printf("[rpeat®] Shutting down server listening on :%s with pid: %d", port, pid)
		if err = syscallKill(pid, syscall.SIGINT); err != nil {
			ServerLogger.Printf("[stop rpeat®] error: %s", err.Error())
		}
	}
	watchFile(pidfile, fsnotify.Remove) // blocks
	time.Sleep(time.Second * 1)         // time for OS to reclaim pid - this may be an issue to resecure?
	ServerLogger.Println("Shutdown command completed")

}

func watchFile(f string, op fsnotify.Op) { // should take fsnotify.Op
	watcher, err := fsnotify.NewWatcher()
	if err != nil {

	}
	defer watcher.Close()

	done := make(chan bool)
	go func() {
		for {
			select {
			case evt, ok := <-watcher.Events:
				if !ok {
					return
				}
				if evt.Op&op == op {
					ServerLogger.Printf("PID file %s %s\n", f, op)
					done <- true
				}
			}
		}
	}()

	err = watcher.Add(f)
	if err != nil {
		ServerLogger.Println("error attempting to watch pid file")
	}
	<-done
}

func nlines(r io.Reader) (int, error) {
	buf := make([]byte, 32*1024)
	count := 0
	lineSep := []byte{'\n'}

	for {
		c, err := r.Read(buf)
		count += bytes.Count(buf[:c], lineSep)

		switch {
		case err == io.EOF:
			return count, nil

		case err != nil:
			return count, err
		}
	}
}

func tailOffset(file string, N int) (offset int64) {
	fh, err := os.Open(file)
	if err != nil {
		ServerLogger.Printf("[tailOffset] error opening file: %s", file)
		return -1
	}
	defer fh.Close()

	length, err := fh.Seek(0, 2) // get file size
	if err != nil || err == io.EOF {
		ServerLogger.Printf("[tailOffset] seek error on file: %s", file)
		return -1
	}

	var bufSize int64 = 4096

	bufSize = int64(math.Min(float64(length), float64(bufSize)))
	buf := make([]byte, bufSize)
	offset = int64(math.Max(float64(length-bufSize), float64(0)))
	nbytes, err := fh.ReadAt(buf, offset)
	if err != nil {
		panic(err)
	}
	var nreads int64 = 1

	var b byte
	var bytes_p int64 = 0
	char_p := 0
	lines_p := 0

LINES:
	for {
		for i := nbytes - 1; i >= 0; i-- {
			b = buf[i]
			if b == 10 {
				//fmt.Printf("%2d(%2d) newline> %3d\t%b\t%02x\t\\n\t%03o\n",i,bytes_p,b,b,b,b)
				lines_p++
				if lines_p == N+1 {
					bytes_p--
					break LINES
				}
				char_p = 0
			} else {
				//fmt.Printf("%2d(%2d)          %3d\t%b\t%x\t%c\t%03o\n",i,bytes_p,b,b,b,b,b)
				char_p++
			}
			bytes_p++
		}
		if lines_p == N {
			break
		}
		if bytes_p <= length || lines_p < N {
			if bytes_p == length {
				break
				//log.Println("ran out of lines in file! - thats all there is")
			} else {
				nreads++
				offset := int64(math.Max(float64(length-bufSize*nreads), float64(0)))
				buf = make([]byte, int64(math.Min(float64(bufSize), float64(length-bytes_p))))
				nbytes, err = fh.ReadAt(buf, offset)
				//log.Printf("offset: %d char_p: %d", offset, char_p)
				//log.Printf("out of buffer - need more bytes - read %d additional bytes", nbytes)
				if err != nil {
					panic(err)
				}
				char_p = 0
				continue
			}
		}
		break
	}

	offset = int64(math.Max(float64(0), float64(length-bytes_p-1)))

	return offset

}

// FIXME: need error handling
func tailLog(f string, N int) string {
	offset := tailOffset(f, N)
	ServerLogger.Printf("readlin last %d lines of %s = offset:%d", N, f, offset)
	if offset < 0 {
		return ""
	}
	fh, err := os.Open(f)
	if err != nil {
		ServerLogger.Println(err)
		return ""
	}
	defer fh.Close()

	_, err = fh.Seek(offset, 0)
	if err != nil {
		ServerLogger.Println(err)
		return ""
	}
	scanner := bufio.NewScanner(fh)

	var content string
	for scanner.Scan() { // TODO: facilitate streaming content?
		content = fmt.Sprintf("%s%s\n", content, scanner.Text())
	}
	return content
}

func Stringify(m interface{}) string {
	return StringifyWithSep(m, ",")
}
func StringifyWithSep(m interface{}, sep string) string {
	var ret string

	switch value := m.(type) {
	case string:
		ret = value
	case []string:
		ret = strings.Join(value, sep+" ")
	case EnvList:
		for i, v := range value {
			if i == 0 {
				ret = v
			} else {
				ret = ret + sep + " " + v
			}
		}
	case map[string]string:
		var x []string
		for k, v := range value {
			x = append(x, fmt.Sprintf("%s=%s", k, v))
		}
		ret = strings.Join(x, sep+" ")
	case map[string][]string:
		x := make([]string, 0)
		for k, v := range value {
			if len(v) > 0 {
				vs := strings.Join(v, sep+" ")
				x = append(x, fmt.Sprintf("%s=[%s]", k, vs))
			}
		}
		ret = strings.Join(x, sep+" ")
	case Permission: // map[string][]string but can't reside in above due to compile error
		x := make([]string, 0)
		for k, v := range value {
			if len(v) > 0 {
				vs := strings.Join(v, sep+" ")
				x = append(x, fmt.Sprintf("%s=[%s]", k, vs))
			}
		}
		ret = strings.Join(x, sep+" ")
	}
	return ret
}

func evaluatedCmd(job *Job, shutdown bool, asof string) (exec.Cmd, error) {
	var err error
	var vErrors []ValidationError
	var vWarnings []ValidationWarning
	var v expandAllVarsOutput

	var cmd *string
	var cmdString string
	if shutdown {
		cmdString = "ShutdownCmd"
		v, _ = expandAllVars(job, []string{}, &job.ShutdownCmd, true, asof)
	} else {
		cmdString = "Cmd"
		cmd = job.Cmd
		//if job.Cmd == nil {
		//  Cmd := ""
		//  cmd = &Cmd
		//}
		v, _ = expandAllVars(job, []string{}, cmd, false, asof)
	}

	if v.execErr != nil {
		vErrors = append(vErrors, ValidationError{JobName: job.Name, Msg: v.execErr.Error(), Exception: Exec})
	}
	if len(v.envVarsMissing) > 0 || len(v.cmdVarsMissing) > 0 {
		var envvars, cmdvars string

		if len(v.envVarsMissing) > 0 {
			envvars = fmt.Sprintf("Warning: Environment variables requested but not defined: %s", Stringify(v.envVarsMissing))
			for _, missing := range v.envVarsMissing {
				vWarnings = append(vWarnings, ValidationWarning{JobName: job.Name, Msg: missing, Exception: EnvVar})
			}
		}
		if len(v.cmdVarsMissing) > 0 {
			cmdvars = fmt.Sprintf("Error: Variables used in %s but not defined: %s", cmdString, Stringify(v.cmdVarsMissing))
			for _, missing := range v.cmdVarsMissing {
				vErrors = append(vErrors, ValidationError{JobName: job.Name, Msg: missing, Exception: CmdVar})
			}
		}
		err = &ParseError{err: []string{envvars, cmdvars}, warn: true}
	}
	job.jve = appendJobValidationExceptions(job.jve, JobValidationExceptions{Warnings: vWarnings, Errors: vErrors})
	return v.execCmd, err
}

func (job *Job) ExpandEnv(vars []string, asof string) []string {
	v, err := expandAllVars(job, vars, job.Cmd, false, asof)
	if err != nil {
		ServerLogger.Printf("WARNING: Environment Variables required but undefined: %v", v.envVarsMissing)
	}
	return v.expandedVars
}

// FIXME: should add all path expansion/file attribute checks - TZ, file perm, path exists, vars, dateenv, executable, shell check
type expandAllVarsOutput struct {
	expandedVars     []string
	execCmd          exec.Cmd
	envVarsMissing   []string
	cmdVarsMissing   []string
	numberCmdVars    int
	dateEnvException error
	execErr          error
}

func stripVars(s string) ([]string, string) {
	re_vars := regexp.MustCompile(".*?\\${?([_0-9a-zA-Z]*)}?.*?")
	re_novars := regexp.MustCompile("(.*?)\\${?[_0-9a-zA-Z]*}?(.*?)")
	return re_vars.FindAllString(s, -1), re_novars.ReplaceAllString(s, "")
}

func expandAllVars(job *Job, vars []string, cmd *string, shutdown bool, asof string) (expandAllVarsOutput, error) {

	evo := expandAllVarsOutput{}

	env := os.Environ()
	env_original := make([]string, len(env))
	copy(env_original, env)

	JobEnv := []string{"RPEAT_TMP=" + job.TmpDir,
		"RPEAT_JOBID=" + job.JobUUID.String(),
		"RPEAT_RUNID=" + job.RunUUID.String(),
		"RPEAT_TIMESTAMP=" + strconv.FormatInt(time.Now().Unix(), 10)}

	env = append(env, JobEnv...)

	EnvMap := make(map[string]string) // map built sequentially from DateEnv/Env slices

	// parse dates into values
	for _, keyval := range job.DateEnv {
		kv := strings.Split(string(keyval), "=")
		dname := kv[0]
		dval := kv[1]
		convertedDate, _ := ConvertDate(dval, job.Timezone, job.CalendarDirs, asof)
		newkv := fmt.Sprintf("%s=%s", dname, convertedDate)
		env = append(env, newkv)
	}
	for _, e := range job.Env {
		env = append(env, string(e))
	}
	// custom getter function to look up in defined environmemt before shell env
	getenv := func(key string) string {
		var v string
		var ok bool
		v, ok = EnvMap[key]
		if !ok {
			v = os.Getenv(key)
		}
		return v
	}

	// resolve *in order* all variables - allowing for expansions to include recursively set vars
	var missing []string // if a variable is not in prior declaration or in the environment, it will be returned here
	for i, keyval := range env {
		kv := strings.Split(string(keyval), "=")
		k := kv[0]
		if len(kv) == 1 {
			// FIXME: no assignment should through a warning
			continue
		}
		v := kv[1]
		newv := os.Expand(v, getenv)
		allvars, novars := stripVars(v)
		if newv == novars && len(allvars) > 0 {
			missing = append(missing, keyval)
		}
		env[i] = fmt.Sprintf("%s=%s", k, newv)
		EnvMap[k] = newv
	}
	evo.envVarsMissing = missing

	// create Cmd
	cmdvars := 0            // number of expanded vars in Cmd
	var cmdmissing []string // list of vars in Cmd that are not defined (i.e. possible issue)
	var args []string
	if cmd != nil {
		args = strings.Fields(*cmd)
	}
	c := exec.Cmd{}
	if len(args) > 0 {
		c.Path = args[0]
		if len(args) > 1 {
			c.Args = []string{args[0], args[1], strings.Join(args[2:], " ")}
		} else {
			c.Args = args[:]
		}
		c.Env = env
		cmdEvalString := os.Expand(strings.Join(c.Args, " "),
			func(key string) string {
				cmdvars++
				v := getenv(key)
				if v == "" {
					cmdmissing = append(cmdmissing, key)
				}
				return v
			})
		if shutdown {
			job.ShutdownCmdEval = cmdEvalString
		} else {
			job.CmdEval = cmdEvalString
		}
		evo.execErr = isExecutable(os.Expand(c.Path, getenv))
	}
	evo.execCmd = c
	evo.cmdVarsMissing = cmdmissing
	evo.numberCmdVars = cmdvars

	// now get newly set variables
	var expandedVars []string
	for _, v := range vars {
		expandedVars = append(expandedVars, os.Expand(v, getenv))
	}
	evo.expandedVars = expandedVars

	// FIXME: move this above cmd checks, and fill in after
	//  evo := expandAllVarsOutput{
	//      expandedVars:expandedVars,
	//      execCmd:c,
	//      envVarsMissing:missing,
	//      cmdVarsMissing:cmdmissing,
	//      numberCmdVars:cmdvars,
	//  }
	//  evo.execErr = isExecutable(os.ExpandEnv(c.Path))

	return evo, nil

}

// exec.findExecutable
func isExecutable(file string) error {
	d, err := os.Stat(file)
	if err != nil {
		return err
	}
	if m := d.Mode(); !m.IsDir() && m&0111 != 0 {
		return nil
	}
	return os.ErrPermission
}
