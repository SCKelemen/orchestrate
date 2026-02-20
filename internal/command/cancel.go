package command

import (
	"fmt"
	"io"
	"net/http"

	"github.com/SCKelemen/clix/v2"
)

func newCancelCmd() *clix.Command {
	cmd := clix.NewCommand("cancel")
	cmd.Short = "Cancel a queued or running task"

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
		resp, err := cc.APIRequest("POST", "/v1/tasks/"+taskID+":cancel", nil)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		out, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("server returned %d: %s", resp.StatusCode, out)
		}

		fmt.Fprintf(ctx.App.Out, "Cancelled task: tasks/%s\n", taskID)
		return nil
	}

	return cmd
}
