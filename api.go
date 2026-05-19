package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const apiBase = "https://iq4server.rainbird.com/coreapi/api"

// Client is the IQ4 API client.
type Client struct {
	token      string
	httpClient *http.Client
}

// NewClient creates a new IQ4 API client with the given JWT token.
func NewClient(token string) *Client {
	return &Client{token: token, httpClient: &http.Client{}}
}

func (c *Client) request(method, path string, body any) ([]byte, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, apiBase+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(data))
	}

	return data, nil
}

func (c *Client) get(path string, result any) error {
	data, err := c.request("GET", path, nil)
	if err != nil {
		return err
	}
	if result != nil && len(data) > 0 {
		return json.Unmarshal(data, result)
	}
	return nil
}

// GetSites returns all sites for the current company.
func (c *Client) GetSites() ([]Site, error) {
	var sites []Site
	return sites, c.get("/Site/GetSites", &sites)
}

// GetControllers returns all controllers across all sites.
func (c *Client) GetControllers() ([]Controller, error) {
	var controllers []Controller
	return controllers, c.get("/Satellite/GetSatelliteList", &controllers)
}

// GetConnectionStatuses returns the MQTT connection status for the given controller IDs.
func (c *Client) GetConnectionStatuses(ids []int) ([]ConnectionStatus, error) {
	params := make([]string, len(ids))
	for i, id := range ids {
		params[i] = fmt.Sprintf("satelliteIds=%d", id)
	}
	var resp ConnectionStatusResponse
	err := c.get("/Satellite/isConnected?"+strings.Join(params, "&"), &resp)
	return resp.Satellites, err
}

// GetPrograms returns all programs for a controller.
func (c *Client) GetPrograms(satelliteID int) ([]Program, error) {
	var programs []Program
	return programs, c.get(fmt.Sprintf("/Program/GetProgramList?satelliteId=%d", satelliteID), &programs)
}

// GetAllPrograms returns all programs across all sites.
func (c *Client) GetAllPrograms() ([]Program, error) {
	var programs []Program
	return programs, c.get("/Program/GetProgramListForMultiSites", &programs)
}

// GetProgramDetail returns the full program object (needed for updates).
func (c *Client) GetProgramDetail(programID int) (ProgramDetail, error) {
	var detail ProgramDetail
	return detail, c.get(fmt.Sprintf("/Program/GetProgram?programId=%d", programID), &detail)
}

// GetStartTimes returns all start times across all programs.
func (c *Client) GetStartTimes() ([]StartTime, error) {
	var times []StartTime
	return times, c.get("/StartTime/GetAllStartTimes?includeProgram=false&includeProgramGroup=false", &times)
}

// GetScheduledStartTimes returns start times grouped by program for a controller.
func (c *Client) GetScheduledStartTimes(satelliteID int) (json.RawMessage, error) {
	data, err := c.request("GET", fmt.Sprintf("/Program/GetScheduledStartTimes?satelliteId=%d", satelliteID), nil)
	return data, err
}

// GetStations returns all stations for a controller.
func (c *Client) GetStations(satelliteID int) ([]Station, error) {
	var stations []Station
	return stations, c.get(fmt.Sprintf("/Station/GetStationListForSatellite?satelliteId=%d", satelliteID), &stations)
}

// GetStationRuntimes returns runtime assignments per station for a controller.
func (c *Client) GetStationRuntimes(satelliteID int) ([]StationRuntime, error) {
	var runtimes []StationRuntime
	return runtimes, c.get("/ProgramStep/GetProgramsAssignedAndRunTimeBySatelliteId?satelliteId="+fmt.Sprint(satelliteID), &runtimes)
}

// GetProgramStep returns the full program step detail (needed for updates).
func (c *Client) GetProgramStep(stepID int) (ProgramStep, error) {
	var step ProgramStep
	return step, c.get(fmt.Sprintf("/ProgramStep/GetProgramStepById?programStepId=%d", stepID), &step)
}

// UpdateProgram sends a full program object to update it.
func (c *Client) UpdateProgram(detail ProgramDetail) error {
	_, err := c.request("PUT", "/Program/UpdateProgram", detail)
	return err
}

// UpdateProgramStep updates a program step (runtime).
func (c *Client) UpdateProgramStep(step ProgramStep) error {
	_, err := c.request("PUT", "/ProgramStep/UpdateProgramStep", step)
	return err
}

// CreateStartTime creates a new start time for a program.
func (c *Client) CreateStartTime(st StartTime) (*StartTime, error) {
	data, err := c.request("POST", "/StartTime/CreateStartTime", st)
	if err != nil {
		return nil, err
	}
	var created StartTime
	return &created, json.Unmarshal(data, &created)
}

// DeleteStartTime removes a start time by ID using the v2 batch endpoint.
func (c *Client) DeleteStartTime(programID, startTimeID int) error {
	body := map[string]any{
		"add":    []any{},
		"update": []any{},
		"delete": map[string]any{
			"id":  programID,
			"ids": []int{startTimeID},
		},
	}
	_, err := c.request("PATCH", "/StartTime/v2/UpdateBatches", body)
	return err
}

// UpdateProgramFields patches program fields (e.g. name) via /Program/UpdateBatches.
// This is the same endpoint the IQ4 app uses — does not call UpdateProgram PUT.
func (c *Client) UpdateProgramFields(programID int, fields map[string]any) error {
	type patch struct {
		Op    string `json:"op"`
		Path  string `json:"path"`
		Value any    `json:"value"`
	}
	patches := []patch{}
	for k, v := range fields {
		patches = append(patches, patch{Op: "replace", Path: "/" + k, Value: v})
	}
	body := map[string]any{
		"ids":   []int{programID},
		"patch": patches,
	}
	_, err := c.request("PATCH", "/Program/UpdateBatches", body)
	return err
}

// UpdateProgramStepBatches sets baseRunTime (in seconds) for one or more program steps
// via /ProgramStep/v3/UpdateBatches — the same endpoint the IQ4 app uses.
func (c *Client) UpdateProgramStepBatches(steps map[int]int) error {
	type entry struct {
		ID    int    `json:"id"`
		Op    string `json:"op"`
		Path  string `json:"path"`
		Value int    `json:"value"`
	}
	entries := []entry{}
	for stepID, seconds := range steps {
		entries = append(entries, entry{ID: stepID, Op: "replace", Path: "/baseRunTime", Value: seconds})
	}
	body := map[string]any{"patch": entries}
	_, err := c.request("PATCH", "/ProgramStep/v3/UpdateBatches", body)
	return err
}

// SetStartTimes atomically replaces start times for a program in a single UpdateBatches call.
// deleteIDs: existing start time IDs to remove. addTimes: "HH:MM" strings to create.
// Uses the same PATCH endpoint the IQ4 app uses — does not call UpdateProgram.
func (c *Client) SetStartTimes(programID int, deleteIDs []int, addTimes []string) error {
	type patch struct {
		Op    string `json:"op"`
		Path  string `json:"path"`
		Value any    `json:"value"`
	}
	type addEntry struct {
		ID    int     `json:"id"`
		Patch []patch `json:"patch"`
	}

	adds := []addEntry{}
	today := time.Now().Format("2006-01-02")
	for _, t := range addTimes {
		adds = append(adds, addEntry{
			ID: 0,
			Patch: []patch{
				{Op: "add", Path: "/dateTimeLocal", Value: fmt.Sprintf("%sT%s:00", today, t)},
				{Op: "add", Path: "/enabled", Value: true},
				{Op: "add", Path: "/programId", Value: programID},
			},
		})
	}

	if deleteIDs == nil {
		deleteIDs = []int{}
	}

	body := map[string]any{
		"add":    adds,
		"update": []any{},
		"delete": map[string]any{
			"id":  programID,
			"ids": deleteIDs,
		},
	}
	_, err := c.request("PATCH", "/StartTime/v2/UpdateBatches", body)
	return err
}

// DeleteProgramSteps removes program steps by ID.
func (c *Client) DeleteProgramSteps(ids []int) error {
	_, err := c.request("DELETE", "/ProgramStep/DeleteProgramSteps", ids)
	return err
}

// CreateProgramSteps creates new program steps.
// Accepts raw JSON payloads matching the UI format.
func (c *Client) CreateProgramSteps(steps any) error {
	_, err := c.request("POST", "/ProgramStep/CreateProgramSteps", steps)
	return err
}
