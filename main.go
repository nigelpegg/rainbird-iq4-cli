// iq4-cli – Rain Bird IQ4 command-line tool.
//
// All output is JSON for easy parsing by scripts and LLMs.
//
// Usage:
//
//	iq4-cli login <username> <password>       Authenticate and store token
//	iq4-cli logout                            Clear stored token
//	iq4-cli save-token                        Read a JWT from stdin and save it (browser fallback)
//	iq4-cli sites                             List all sites
//	iq4-cli controllers                       List all controllers with connection status
//	iq4-cli stations <controller-id>          List stations for a controller
//	iq4-cli programs                          List all programs across all controllers
//	iq4-cli programs <controller-id>          List programs for a specific controller
//	iq4-cli program <program-id>              Show full program detail
//	iq4-cli start-times                       List all start times
//	iq4-cli start-times <controller-id>       List start times for a controller
//	iq4-cli runtimes <controller-id>          List station runtimes for a controller
//	iq4-cli set-adjust <program-id> <percent> Set seasonal adjust percentage
//	iq4-cli set-days <program-id> <days>      Set water days (e.g. "MoTuWeThFrSaSu" or "1111111")
//	iq4-cli set-runtime <step-id> <duration>  Set base runtime (e.g. "10m", "1h30m")
//	iq4-cli set-details <program-id> <name> [field=value ...] [addStart=HH:MM ...]  Update program; addStart embeds start times atomically
//	iq4-cli stop-irrigation <satellite-id>        Stop all active irrigation on a controller
//	iq4-cli set-runtimes [step-id=duration ...]  Set baseRunTime for steps via /ProgramStep/v3/UpdateBatches (IQ4-app-compatible)
//	iq4-cli update-program <program-id> [field=value ...]  Patch program fields via /Program/UpdateBatches (IQ4-app-compatible)
//	iq4-cli set-starts <program-id> [del=<start-time-id> ...] [time=HH:MM ...]  Atomically replace start times (IQ4-app-compatible)
//	iq4-cli add-start <program-id> <time>     Add a start time (e.g. "06:00")
//	iq4-cli del-start <start-time-id>         Delete a start time
//	iq4-cli add-step <program-id> <station-id> Assign a station to a program
//	iq4-cli del-step <step-id>               Remove a station from a program
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "login":
		cmdLogin(args)
	case "logout":
		cmdLogout()
	case "save-token":
		cmdSaveToken()
	case "sites":
		cmdSites()
	case "controllers":
		cmdControllers()
	case "stations":
		cmdStations(args)
	case "programs":
		cmdPrograms(args)
	case "program":
		cmdProgram(args)
	case "start-times":
		cmdStartTimes(args)
	case "runtimes":
		cmdRuntimes(args)
	case "set-adjust":
		cmdSetAdjust(args)
	case "set-days":
		cmdSetDays(args)
	case "set-details":
		cmdSetDetails(args)
	case "set-runtime":
		cmdSetRuntime(args)
	case "stop-irrigation":
		cmdStopIrrigation(args)
	case "set-runtimes":
		cmdSetRuntimes(args)
	case "update-program":
		cmdUpdateProgram(args)
	case "set-starts":
		cmdSetStarts(args)
	case "add-start":
		cmdAddStart(args)
	case "del-start":
		cmdDelStart(args)
	case "add-step":
		cmdAddStep(args)
	case "del-step":
		cmdDelStep(args)
	case "help", "-h", "--help":
		printUsage()
	default:
		fatalf("unknown command: %s", cmd)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `iq4-cli – Rain Bird IQ4 command-line tool

All output is JSON for easy parsing by scripts and LLMs.

Commands:
  login <username> <password>       Authenticate and store token
  logout                            Clear stored token
  save-token                        Read a JWT from stdin and save it (browser fallback)

  sites                             List all sites
  controllers                       List all controllers with connection status
  stations <controller-id>          List stations for a controller

  programs                          List all programs across all controllers
  programs <controller-id>          List programs for a specific controller
  program <program-id>              Show full program detail (with start times and runtimes)

  start-times                       List all start times
  start-times <controller-id>       List start times for a controller
  runtimes <controller-id>          List station runtimes for a controller

  set-adjust <program-id> <percent> Set seasonal adjust percentage (e.g. 130 for 130%%)
  set-days <program-id> <days>      Set water days (e.g. "MoTuWeThFr", "MoWeFr", "1010100")
  set-runtime <step-id> <duration>  Set base runtime (e.g. "10m", "1h30m", "0h15m")
  set-details <program-id> <name>      Rename a program
  add-start <program-id> <time> [company-id]  Add a start time (e.g. "06:00"); company-id avoids an extra API call
  del-start <program-id> <start-time-id>  Delete a start time
  add-step <program-id> <station-id> Assign a station to a program
  del-step <step-id>                 Remove a station from a program

Data model:
  Company → Sites → Controllers → Stations (physical valve zones)
                                 → Programs (A, B, C irrigation schedules)
                                     → Start times (when to run)
                                     → Program steps (station → runtime)
                                     → Seasonal adjust (%% scaling)
`)
}

func requireClient() *Client {
	token := LoadToken()
	if token == "" {
		fatalf("not logged in – run: iq4-cli login <username> <password>")
	}
	return NewClient(token)
}

func requireArg(args []string, n int, usage string) {
	if len(args) < n {
		fatalf("usage: iq4-cli %s", usage)
	}
}

func requireInt(s, label string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		fatalf("invalid %s: %s", label, s)
	}
	return n
}

func output(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}

func check(err error) {
	if err != nil {
		fatalf("%v", err)
	}
}

// --- Commands ---

func cmdLogin(args []string) {
	requireArg(args, 2, "login <username> <password>")
	token, err := Authenticate(args[0], args[1])
	check(err)

	// Validate token
	client := NewClient(token)
	_, err = client.GetSites()
	check(err)

	check(SaveToken(token))
	fmt.Fprintf(os.Stderr, "logged in successfully\n")
}

func cmdLogout() {
	ClearToken()
	fmt.Fprintf(os.Stderr, "logged out\n")
}

func cmdSaveToken() {
	fmt.Fprintf(os.Stderr, "Paste your JWT token and press Enter (or Ctrl-D):\n")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	var token string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			token = line
			break
		}
	}
	if token == "" {
		fatalf("no token provided")
	}
	// Validate it works
	client := NewClient(token)
	_, err := client.GetSites()
	check(err)
	check(SaveToken(token))
	fmt.Fprintf(os.Stderr, "token saved and validated successfully\n")
}

func cmdSites() {
	c := requireClient()
	sites, err := c.GetSites()
	check(err)
	output(sites)
}

func cmdControllers() {
	c := requireClient()
	controllers, err := c.GetControllers()
	check(err)

	// Get connection statuses
	ids := make([]int, len(controllers))
	for i, ctrl := range controllers {
		ids[i] = ctrl.ID
	}
	statuses, err := c.GetConnectionStatuses(ids)
	check(err)

	statusMap := make(map[int]bool)
	for _, s := range statuses {
		statusMap[s.ID] = s.IsConnected
	}

	type ControllerWithStatus struct {
		Controller
		IsConnected bool `json:"isConnected"`
	}

	result := make([]ControllerWithStatus, len(controllers))
	for i, ctrl := range controllers {
		result[i] = ControllerWithStatus{
			Controller:  ctrl,
			IsConnected: statusMap[ctrl.ID],
		}
	}
	output(result)
}

func cmdStations(args []string) {
	requireArg(args, 1, "stations <controller-id>")
	c := requireClient()
	id := requireInt(args[0], "controller-id")
	stations, err := c.GetStations(id)
	check(err)
	output(stations)
}

func cmdPrograms(args []string) {
	c := requireClient()
	if len(args) > 0 {
		id := requireInt(args[0], "controller-id")
		programs, err := c.GetPrograms(id)
		check(err)
		output(programs)
	} else {
		programs, err := c.GetAllPrograms()
		check(err)
		output(programs)
	}
}

func cmdProgram(args []string) {
	requireArg(args, 1, "program <program-id>")
	c := requireClient()
	id := requireInt(args[0], "program-id")

	detail, err := c.GetProgramDetail(id)
	check(err)

	// Also get start times for this program
	allTimes, err := c.GetStartTimes()
	check(err)

	var programTimes []StartTime
	for _, st := range allTimes {
		if st.ProgramID == id {
			programTimes = append(programTimes, st)
		}
	}

	// Get runtimes if we have a satellite ID
	satID, _ := detail["satelliteId"].(float64)
	var runtimes []StationRuntime
	if satID > 0 {
		runtimes, err = c.GetStationRuntimes(int(satID))
		check(err)
	}

	// Filter runtimes to this program only
	type StationRuntimeFiltered struct {
		StationID int                 `json:"stationId"`
		Runtimes  []RuntimeAssignment `json:"runtimes"`
	}
	var filtered []StationRuntimeFiltered
	for _, sr := range runtimes {
		var matching []RuntimeAssignment
		for _, rt := range sr.RuntimeProgramAssignedList {
			if rt.ProgramID == id {
				matching = append(matching, rt)
			}
		}
		if len(matching) > 0 {
			filtered = append(filtered, StationRuntimeFiltered{
				StationID: sr.StationID,
				Runtimes:  matching,
			})
		}
	}

	// Get station names
	var stations []Station
	if satID > 0 {
		stations, _ = c.GetStations(int(satID))
	}
	stationMap := make(map[int]string)
	for _, s := range stations {
		stationMap[s.ID] = s.Name
	}

	result := map[string]any{
		"program":    detail,
		"startTimes": programTimes,
		"runtimes":   filtered,
		"stations":   stationMap,
	}
	output(result)
}

func cmdStartTimes(args []string) {
	c := requireClient()
	if len(args) > 0 {
		id := requireInt(args[0], "controller-id")
		data, err := c.GetScheduledStartTimes(id)
		check(err)
		os.Stdout.Write(data)
		fmt.Println()
	} else {
		times, err := c.GetStartTimes()
		check(err)
		output(times)
	}
}

func cmdRuntimes(args []string) {
	requireArg(args, 1, "runtimes <controller-id>")
	c := requireClient()
	id := requireInt(args[0], "controller-id")

	runtimes, err := c.GetStationRuntimes(id)
	check(err)

	// Enrich with station names
	stations, _ := c.GetStations(id)
	stationMap := make(map[int]string)
	for _, s := range stations {
		stationMap[s.ID] = s.Name
	}

	type EnrichedRuntime struct {
		StationID   int                 `json:"stationId"`
		StationName string              `json:"stationName"`
		Runtimes    []RuntimeAssignment `json:"runtimes"`
	}
	result := make([]EnrichedRuntime, len(runtimes))
	for i, sr := range runtimes {
		result[i] = EnrichedRuntime{
			StationID:   sr.StationID,
			StationName: stationMap[sr.StationID],
			Runtimes:    sr.RuntimeProgramAssignedList,
		}
	}
	output(result)
}

func cmdSetAdjust(args []string) {
	requireArg(args, 2, "set-adjust <program-id> <percent>")
	c := requireClient()
	id := requireInt(args[0], "program-id")
	pct := requireInt(args[1], "percent")

	detail, err := c.GetProgramDetail(id)
	check(err)

	detail["programAdjust"] = pct
	delete(detail, "startTime")
	delete(detail, "programStep")
	check(c.UpdateProgram(detail))

	fmt.Fprintf(os.Stderr, "set seasonal adjust to %d%% for program %d\n", pct, id)
}

func cmdSetDetails(args []string) {
	requireArg(args, 2, "set-details <program-id> <name> [field=value ...] [addStart=HH:MM ...]")
	c := requireClient()
	id := requireInt(args[0], "program-id")
	name := args[1]

	detail, err := c.GetProgramDetail(id)
	check(err)

	detail["name"] = name

	// Optional extra field=value overrides applied to the same UpdateProgram call.
	// Values that parse as integers are stored as numbers; otherwise as strings.
	// Fields in stringFields are always kept as strings regardless of value.
	// addStart=HH:MM embeds start times directly in the UpdateProgram body so the
	// controller receives them atomically in the same MQTT push.
	stringFields := map[string]bool{
		"weekDays":              true, // 7-char binary e.g. "1111111" — must stay string
		"nextCyclicalStartDate": true, // ISO datetime string
	}
	var embeddedStarts []StartTime
	companyID := 0
	if cid, ok := detail["companyId"].(float64); ok {
		companyID = int(cid)
	}
	for _, kv := range args[2:] {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) != 2 {
			fatalf("invalid field override %q: expected key=value", kv)
		}
		k, v := parts[0], parts[1]
		if k == "addStart" {
			t := parseTime(v)
			embeddedStarts = append(embeddedStarts, StartTime{
				DateTime:  fmt.Sprintf("1999-09-09T%s:00", t),
				ProgramID: id,
				Enabled:   true,
				CompanyID: companyID,
			})
			continue
		}
		if !stringFields[k] {
			if n, err := strconv.Atoi(v); err == nil {
				detail[k] = n
				continue
			}
		}
		detail[k] = v
	}

	// If start times were provided via addStart=, embed them in the UpdateProgram body
	// so they are set atomically and the controller receives them in the same MQTT push.
	// Otherwise strip startTime — GetProgram returns it as an empty array and sending
	// that back clears any existing start times.
	if len(embeddedStarts) > 0 {
		detail["startTime"] = embeddedStarts
	} else {
		delete(detail, "startTime")
	}
	delete(detail, "programStep")

	check(c.UpdateProgram(detail))
	fmt.Fprintf(os.Stderr, "updated program %d\n", id)
}

func cmdSetDays(args []string) {
	requireArg(args, 2, "set-days <program-id> <days>")
	c := requireClient()
	id := requireInt(args[0], "program-id")
	days := parseDays(args[1])

	detail, err := c.GetProgramDetail(id)
	check(err)

	detail["weekDays"] = days
	delete(detail, "startTime")
	delete(detail, "programStep")
	check(c.UpdateProgram(detail))

	fmt.Fprintf(os.Stderr, "set water days to %s for program %d\n", formatDays(days), id)
}

func cmdSetRuntime(args []string) {
	requireArg(args, 2, "set-runtime <step-id> <duration>")
	c := requireClient()
	id := requireInt(args[0], "step-id")
	dur := parseDuration(args[1])

	step, err := c.GetProgramStep(id)
	check(err)

	step.RunTime = formatTimeSpan(dur)
	step.RunTimeLong = dur.Nanoseconds() / 100 // .NET ticks (100ns)
	check(c.UpdateProgramStep(step))

	fmt.Fprintf(os.Stderr, "set runtime to %s for step %d\n", dur, id)
}

func cmdStopIrrigation(args []string) {
	requireArg(args, 1, "stop-irrigation <satellite-id>")
	c := requireClient()
	id := requireInt(args[0], "satellite-id")
	check(c.StopAllIrrigation(id))
	fmt.Fprintf(os.Stderr, "stopped all irrigation on satellite %d\n", id)
}

func cmdSetRuntimes(args []string) {
	requireArg(args, 1, "set-runtimes <step-id=duration> ...")
	c := requireClient()

	steps := map[int]int{}
	for _, arg := range args {
		parts := strings.SplitN(arg, "=", 2)
		if len(parts) != 2 {
			fatalf("invalid argument %q: expected step-id=duration", arg)
		}
		stepID := requireInt(parts[0], "step-id")
		dur := parseDuration(parts[1])
		steps[stepID] = int(dur.Seconds())
	}

	check(c.UpdateProgramStepBatches(steps))
	fmt.Fprintf(os.Stderr, "set runtimes: %v\n", steps)
	output(map[string]any{"updated": steps})
}

func cmdUpdateProgram(args []string) {
	requireArg(args, 2, "update-program <program-id> [field=value ...]")
	c := requireClient()
	id := requireInt(args[0], "program-id")

	fields := map[string]any{}
	for _, kv := range args[1:] {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) != 2 {
			fatalf("invalid argument %q: expected field=value", kv)
		}
		k, v := parts[0], parts[1]
		if n, err := strconv.Atoi(v); err == nil {
			fields[k] = n
		} else {
			fields[k] = v
		}
	}

	check(c.UpdateProgramFields(id, fields))
	fmt.Fprintf(os.Stderr, "updated program %d fields: %v\n", id, fields)
	output(map[string]any{"programId": id, "updated": fields})
}

func cmdSetStarts(args []string) {
	requireArg(args, 1, "set-starts <program-id> [del=<start-time-id> ...] [time=HH:MM ...]")
	c := requireClient()
	programID := requireInt(args[0], "program-id")

	var deleteIDs []int
	var addTimes []string

	for _, arg := range args[1:] {
		parts := strings.SplitN(arg, "=", 2)
		if len(parts) != 2 {
			fatalf("invalid argument %q: expected del=<id> or time=HH:MM", arg)
		}
		switch parts[0] {
		case "del":
			deleteIDs = append(deleteIDs, requireInt(parts[1], "start-time-id"))
		case "time":
			addTimes = append(addTimes, parseTime(parts[1]))
		default:
			fatalf("unknown argument key %q: expected del or time", parts[0])
		}
	}

	check(c.SetStartTimes(programID, deleteIDs, addTimes))
	fmt.Fprintf(os.Stderr, "set start times for program %d: deleted %v, added %v\n", programID, deleteIDs, addTimes)
	output(map[string]any{"programId": programID, "deleted": deleteIDs, "added": addTimes})
}

func cmdAddStart(args []string) {
	requireArg(args, 2, "add-start <program-id> <time> [company-id]")
	c := requireClient()
	programID := requireInt(args[0], "program-id")
	t := parseTime(args[1])

	// companyId is required for the start time to be visible in the app.
	// Callers should pass it directly (avoids an extra API round-trip).
	// If omitted, we fetch it from the program — slower but always correct.
	var companyID int
	if len(args) >= 3 {
		companyID = requireInt(args[2], "company-id")
	} else {
		detail, err := c.GetProgramDetail(programID)
		if err == nil {
			if cid, ok := detail["companyId"].(float64); ok {
				companyID = int(cid)
			}
		}
	}

	st := StartTime{
		DateTime:  fmt.Sprintf("1999-09-09T%s:00", t),
		ProgramID: programID,
		Enabled:   true,
		CompanyID: companyID,
	}
	created, err := c.CreateStartTime(st)
	check(err)

	fmt.Fprintf(os.Stderr, "created start time %d at %s for program %d (companyId=%d)\n", created.ID, t, programID, companyID)
	output(created)
}

func cmdDelStart(args []string) {
	requireArg(args, 2, "del-start <program-id> <start-time-id>")
	c := requireClient()
	programID := requireInt(args[0], "program-id")
	startTimeID := requireInt(args[1], "start-time-id")

	check(c.DeleteStartTime(programID, startTimeID))
	fmt.Fprintf(os.Stderr, "deleted start time %d from program %d\n", startTimeID, programID)
}

func cmdAddStep(args []string) {
	requireArg(args, 2, "add-step <program-id> <station-id>")
	c := requireClient()
	programID := args[0]
	stationID := requireInt(args[1], "station-id")

	steps := []NewProgramStep{{
		ActionID:    "RunStation",
		ProgramID:   programID,
		RunTimeLong: nil,
		StationID:   stationID,
	}}
	check(c.CreateProgramSteps(steps))

	fmt.Fprintf(os.Stderr, "added station %d to program %s\n", stationID, programID)
}

func cmdDelStep(args []string) {
	requireArg(args, 1, "del-step <step-id>")
	c := requireClient()
	id := requireInt(args[0], "step-id")

	check(c.DeleteProgramSteps([]int{id}))
	fmt.Fprintf(os.Stderr, "deleted program step %d\n", id)
}

// --- Helpers ---

// parseDays converts "MoTuWe" or "0110100" to a 7-char binary string.
// IQ4 weekDays format is Su Mo Tu We Th Fr Sa (Sunday-first).
func parseDays(s string) string {
	if len(s) == 7 && strings.ContainsAny(s, "01") {
		return s
	}
	days := [7]byte{'0', '0', '0', '0', '0', '0', '0'}
	// Order: Su Mo Tu We Th Fr Sa
	labels := []string{"Su", "Mo", "Tu", "We", "Th", "Fr", "Sa"}
	upper := strings.ToUpper(s)
	for i, lbl := range labels {
		if strings.Contains(upper, strings.ToUpper(lbl)) {
			days[i] = '1'
		}
	}
	result := string(days[:])
	if result == "0000000" {
		fatalf("invalid days: %s (use e.g. MoWeFr or 0101010)", s)
	}
	return result
}

// dayLabels in IQ4 order: Su Mo Tu We Th Fr Sa
var dayLabels = []string{"Su", "Mo", "Tu", "We", "Th", "Fr", "Sa"}

func formatDays(s string) string {
	if s == "1111111" {
		return "every day"
	}
	var parts []string
	for i, c := range s {
		if c == '1' {
			parts = append(parts, dayLabels[i])
		}
	}
	return strings.Join(parts, " ")
}

func parseDuration(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		// Try HH:MM format
		parts := strings.Split(s, ":")
		if len(parts) == 2 {
			h, _ := strconv.Atoi(parts[0])
			m, _ := strconv.Atoi(parts[1])
			return time.Duration(h)*time.Hour + time.Duration(m)*time.Minute
		}
		fatalf("invalid duration: %s (use e.g. 10m, 1h30m, 0:15)", s)
	}
	return d
}

func formatTimeSpan(d time.Duration) string {
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}

func parseTime(s string) string {
	orig := s
	s = strings.TrimSpace(s)

	// Detect and strip am/pm suffix (case-insensitive)
	pm := false
	am := false
	lower := strings.ToLower(s)
	if strings.HasSuffix(lower, "pm") {
		pm = true
		s = s[:len(s)-2]
	} else if strings.HasSuffix(lower, "am") {
		am = true
		s = s[:len(s)-2]
	}
	_ = am // am just suppresses 12→0 for 12am edge case below

	var h, m int
	if strings.Contains(s, ":") {
		parts := strings.Split(s, ":")
		if len(parts) != 2 {
			fatalf("invalid time: %s", orig)
		}
		h = requireInt(strings.TrimSpace(parts[0]), "hour")
		m = requireInt(strings.TrimSpace(parts[1]), "minute")
	} else {
		h = requireInt(strings.TrimSpace(s), "hour")
		m = 0
	}

	// Apply am/pm
	if pm && h != 12 {
		h += 12
	} else if am && h == 12 {
		h = 0
	}

	if h < 0 || h > 23 || m < 0 || m > 59 {
		fatalf("invalid time: %s", orig)
	}
	return fmt.Sprintf("%02d:%02d", h, m)
}
