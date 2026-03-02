package command

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/SCKelemen/clix/v2"
)

func newStatusCmd() *clix.Command {
	cmd := clix.NewCommand("status")
	cmd.Short = "Get the status of a task"

	var (
		cc     ClientConfig
		taskID string
	)

	cc.RegisterFlags(cmd)
	cmd.Flags.StringVar(clix.StringVarOptions{
		FlagOptions: clix.FlagOptions{Name: "task", Positional: true, Required: true},
		Value:       &taskID,
	})

	cmd.Run = func(ctx *clix.Context) error {
		resp, err := cc.APIRequest("GET", "/v1/tasks/"+taskID, nil)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		out, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("server returned %d: %s", resp.StatusCode, out)
		}

		var task map[string]any
		_ = json.Unmarshal(out, &task)
		pretty, _ := json.MarshalIndent(task, "", "  ")
		_, _ = fmt.Fprintln(ctx.App.Out, string(pretty))
		return nil
	}

	return cmd
}
