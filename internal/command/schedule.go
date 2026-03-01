package command

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"text/tabwriter"

	"github.com/SCKelemen/clix/v2"
)

func newScheduleCmd() *clix.Command {
	cmd := clix.NewCommand("schedule")
	cmd.Short = "Manage scheduled jobs"

	cmd.Children = []*clix.Command{
		newScheduleCreateCmd(),
		newScheduleListCmd(),
		newScheduleGetCmd(),
		newScheduleDeleteCmd(),
		newSchedulePauseCmd(),
		newScheduleResumeCmd(),
	}

	return cmd
}

func newScheduleCreateCmd() *clix.Command {
	cmd := clix.NewCommand("create")
	cmd.Short = "Create a new schedule"

	var (
		cc           ClientConfig
		agent        string
		title        string
		scheduleExpr string
		prompt       string
		repoURL      string
		baseRef      string
		strategy     string
		agentCount   int
		image        string
		maxRuns      int
		fsPaths      string
		network      string
		egress       string
	)

	cc.RegisterFlags(cmd)
	cmd.Flags.StringVar(clix.StringVarOptions{
		FlagOptions: clix.FlagOptions{Name: "agent", EnvVar: "ORCHESTRATE_AGENT"},
		Default:     "claude",
		Value:       &agent,
	})
	cmd.Flags.StringVar(clix.StringVarOptions{
		FlagOptions: clix.FlagOptions{Name: "title"},
		Value:       &title,
	})
	cmd.Flags.StringVar(clix.StringVarOptions{
		FlagOptions: clix.FlagOptions{Name: "schedule", Required: true},
		Value:       &scheduleExpr,
	})
	cmd.Flags.StringVar(clix.StringVarOptions{
		FlagOptions: clix.FlagOptions{Name: "prompt", Short: "p", Required: true},
		Value:       &prompt,
	})
	cmd.Flags.StringVar(clix.StringVarOptions{
		FlagOptions: clix.FlagOptions{Name: "repo", Short: "r", Required: true},
		Value:       &repoURL,
	})
	cmd.Flags.StringVar(clix.StringVarOptions{
		FlagOptions: clix.FlagOptions{Name: "base-ref"},
		Default:     "main",
		Value:       &baseRef,
	})
	cmd.Flags.StringVar(clix.StringVarOptions{
		FlagOptions: clix.FlagOptions{Name: "strategy"},
		Default:     "IMPLEMENT",
		Value:       &strategy,
	})
	cmd.Flags.IntVar(clix.IntVarOptions{
		FlagOptions: clix.FlagOptions{Name: "agents", Short: "n"},
		Default:     "1",
		Value:       &agentCount,
	})
	cmd.Flags.StringVar(clix.StringVarOptions{
		FlagOptions: clix.FlagOptions{Name: "image"},
		Default:     "orchestrate-agent:latest",
		Value:       &image,
	})
	cmd.Flags.IntVar(clix.IntVarOptions{
		FlagOptions: clix.FlagOptions{Name: "max-runs"},
		Default:     "0",
		Value:       &maxRuns,
	})
	cmd.Flags.StringVar(clix.StringVarOptions{
		FlagOptions: clix.FlagOptions{Name: "fs-paths"},
		Value:       &fsPaths,
	})
	cmd.Flags.StringVar(clix.StringVarOptions{
		FlagOptions: clix.FlagOptions{Name: "network-mode"},
		Value:       &network,
	})
	cmd.Flags.StringVar(clix.StringVarOptions{
		FlagOptions: clix.FlagOptions{Name: "egress-domains"},
		Value:       &egress,
	})

	cmd.Run = func(ctx *clix.Context) error {
		body := map[string]any{
			"agent":        agent,
			"title":        title,
			"scheduleExpr": scheduleExpr,
			"prompt":       prompt,
			"repoUrl":      repoURL,
			"baseRef":      baseRef,
			"strategy":     strategy,
			"agentCount":   agentCount,
			"image":        image,
			"maxRuns":      maxRuns,
		}
		if manifest := buildManifestPayload(fsPaths, network, egress); manifest != nil {
			body["manifest"] = manifest
		}

		data, _ := json.Marshal(body)
		resp, err := cc.APIRequest("POST", "/v1/schedules", bytes.NewReader(data))
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		out, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusCreated {
			return fmt.Errorf("server returned %d: %s", resp.StatusCode, out)
		}

		var result map[string]any
		json.Unmarshal(out, &result)
		fmt.Fprintf(ctx.App.Out, "Created schedule: %s\n", result["name"])
		if next, ok := result["nextRunTime"].(string); ok {
			fmt.Fprintf(ctx.App.Out, "Next run: %s\n", next)
		}
		return nil
	}

	return cmd
}

func newScheduleListCmd() *clix.Command {
	cmd := clix.NewCommand("list")
	cmd.Short = "List schedules"

	var (
		cc    ClientConfig
		state string
	)

	cc.RegisterFlags(cmd)
	cmd.Flags.StringVar(clix.StringVarOptions{
		FlagOptions: clix.FlagOptions{Name: "state"},
		Value:       &state,
	})

	cmd.Run = func(ctx *clix.Context) error {
		path := "/v1/schedules"
		if state != "" {
			path += "?state=" + state
		}

		resp, err := cc.APIRequest("GET", path, nil)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		out, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("server returned %d: %s", resp.StatusCode, out)
		}

		var result struct {
			Schedules []struct {
				Name         string `json:"name"`
				Title        string `json:"title"`
				ScheduleExpr string `json:"scheduleExpr"`
				State        string `json:"state"`
				NextRunTime  string `json:"nextRunTime"`
				RunCount     int    `json:"runCount"`
			} `json:"schedules"`
		}
		json.Unmarshal(out, &result)

		w := tabwriter.NewWriter(ctx.App.Out, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tTITLE\tSCHEDULE\tSTATE\tNEXT RUN\tRUNS")
		for _, sc := range result.Schedules {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%d\n",
				sc.Name, sc.Title, sc.ScheduleExpr, sc.State, sc.NextRunTime, sc.RunCount)
		}
		return w.Flush()
	}

	return cmd
}

func newScheduleGetCmd() *clix.Command {
	cmd := clix.NewCommand("get")
	cmd.Short = "Get details of a schedule"

	var (
		cc         ClientConfig
		scheduleID string
	)

	cc.RegisterFlags(cmd)
	cmd.Flags.StringVar(clix.StringVarOptions{
		FlagOptions: clix.FlagOptions{Name: "schedule", Positional: true, Required: true},
		Value:       &scheduleID,
	})

	cmd.Run = func(ctx *clix.Context) error {
		resp, err := cc.APIRequest("GET", "/v1/schedules/"+scheduleID, nil)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		out, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("server returned %d: %s", resp.StatusCode, out)
		}

		var sc map[string]any
		json.Unmarshal(out, &sc)
		pretty, _ := json.MarshalIndent(sc, "", "  ")
		fmt.Fprintln(ctx.App.Out, string(pretty))
		return nil
	}

	return cmd
}

func newScheduleDeleteCmd() *clix.Command {
	cmd := clix.NewCommand("delete")
	cmd.Short = "Delete a schedule"

	var (
		cc         ClientConfig
		scheduleID string
	)

	cc.RegisterFlags(cmd)
	cmd.Flags.StringVar(clix.StringVarOptions{
		FlagOptions: clix.FlagOptions{Name: "schedule", Positional: true, Required: true},
		Value:       &scheduleID,
	})

	cmd.Run = func(ctx *clix.Context) error {
		resp, err := cc.APIRequest("DELETE", "/v1/schedules/"+scheduleID, nil)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNoContent {
			out, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("server returned %d: %s", resp.StatusCode, out)
		}

		fmt.Fprintf(ctx.App.Out, "Deleted schedule: schedules/%s\n", scheduleID)
		return nil
	}

	return cmd
}

func newSchedulePauseCmd() *clix.Command {
	cmd := clix.NewCommand("pause")
	cmd.Short = "Pause a schedule"

	var (
		cc         ClientConfig
		scheduleID string
	)

	cc.RegisterFlags(cmd)
	cmd.Flags.StringVar(clix.StringVarOptions{
		FlagOptions: clix.FlagOptions{Name: "schedule", Positional: true, Required: true},
		Value:       &scheduleID,
	})

	cmd.Run = func(ctx *clix.Context) error {
		resp, err := cc.APIRequest("POST", "/v1/schedules/"+scheduleID+"/:pause", nil)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		out, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("server returned %d: %s", resp.StatusCode, out)
		}

		fmt.Fprintf(ctx.App.Out, "Paused schedule: schedules/%s\n", scheduleID)
		return nil
	}

	return cmd
}

func newScheduleResumeCmd() *clix.Command {
	cmd := clix.NewCommand("resume")
	cmd.Short = "Resume a paused schedule"

	var (
		cc         ClientConfig
		scheduleID string
	)

	cc.RegisterFlags(cmd)
	cmd.Flags.StringVar(clix.StringVarOptions{
		FlagOptions: clix.FlagOptions{Name: "schedule", Positional: true, Required: true},
		Value:       &scheduleID,
	})

	cmd.Run = func(ctx *clix.Context) error {
		resp, err := cc.APIRequest("POST", "/v1/schedules/"+scheduleID+"/:resume", nil)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		out, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("server returned %d: %s", resp.StatusCode, out)
		}

		var result map[string]any
		json.Unmarshal(out, &result)
		fmt.Fprintf(ctx.App.Out, "Resumed schedule: schedules/%s\n", scheduleID)
		if next, ok := result["nextRunTime"].(string); ok {
			fmt.Fprintf(ctx.App.Out, "Next run: %s\n", next)
		}
		return nil
	}

	return cmd
}
