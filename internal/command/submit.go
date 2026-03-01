package command

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/SCKelemen/clix/v2"
)

func newSubmitCmd() *clix.Command {
	cmd := clix.NewCommand("submit")
	cmd.Short = "Submit a new task to the queue"

	var (
		cc         ClientConfig
		agent      string
		title      string
		prompt     string
		repoURL    string
		baseRef    string
		strategy   string
		agentCount int
		priority   int
		image      string
		fsPaths    string
		network    string
		egress     string
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
	cmd.Flags.IntVar(clix.IntVarOptions{
		FlagOptions: clix.FlagOptions{Name: "priority"},
		Default:     "0",
		Value:       &priority,
	})
	cmd.Flags.StringVar(clix.StringVarOptions{
		FlagOptions: clix.FlagOptions{Name: "image"},
		Default:     "orchestrate-agent:latest",
		Value:       &image,
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
			"agent":      agent,
			"title":      title,
			"prompt":     prompt,
			"repoUrl":    repoURL,
			"baseRef":    baseRef,
			"strategy":   strategy,
			"agentCount": agentCount,
			"priority":   priority,
			"image":      image,
		}
		if manifest := buildManifestPayload(fsPaths, network, egress); manifest != nil {
			body["manifest"] = manifest
		}

		data, _ := json.Marshal(body)
		resp, err := cc.APIRequest("POST", "/v1/tasks", bytes.NewReader(data))
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
		fmt.Fprintf(ctx.App.Out, "Created task: %s\n", result["name"])
		return nil
	}

	return cmd
}

func apiRequest(server, token, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method, server+path, body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return http.DefaultClient.Do(req)
}
