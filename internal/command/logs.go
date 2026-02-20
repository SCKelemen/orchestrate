package command

import (
	"bufio"
	"fmt"
	"net/http"
	"strings"

	"github.com/SCKelemen/clix/v2"
)

func newLogsCmd() *clix.Command {
	cmd := clix.NewCommand("logs")
	cmd.Short = "Stream logs for a task run"

	var (
		cc     ClientConfig
		taskID string
		runID  string
	)

	cc.RegisterFlags(cmd)
	cmd.Flags.StringVar(clix.StringVarOptions{
		FlagOptions: clix.FlagOptions{Name: "task", Required: true},
		Value:       &taskID,
	})
	cmd.Flags.StringVar(clix.StringVarOptions{
		FlagOptions: clix.FlagOptions{Name: "run", Required: true},
		Value:       &runID,
	})

	cmd.Run = func(ctx *clix.Context) error {
		path := fmt.Sprintf("/v1/tasks/%s/runs/%s:logs", taskID, runID)
		resp, err := cc.APIRequest("GET", path, nil)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("server returned %d", resp.StatusCode)
		}

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data: ") {
				fmt.Fprintln(ctx.App.Out, strings.TrimPrefix(line, "data: "))
			}
			if strings.HasPrefix(line, "event: done") {
				break
			}
		}
		return scanner.Err()
	}

	return cmd
}
