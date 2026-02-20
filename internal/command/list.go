package command

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"text/tabwriter"

	"github.com/SCKelemen/clix/v2"
)

func newListCmd() *clix.Command {
	cmd := clix.NewCommand("list")
	cmd.Short = "List tasks"

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
		path := "/v1/tasks"
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
			Tasks []struct {
				Name     string `json:"name"`
				Title    string `json:"title"`
				State    string `json:"state"`
				Strategy string `json:"strategy"`
			} `json:"tasks"`
		}
		json.Unmarshal(out, &result)

		w := tabwriter.NewWriter(ctx.App.Out, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tTITLE\tSTATE\tSTRATEGY")
		for _, t := range result.Tasks {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", t.Name, t.Title, t.State, t.Strategy)
		}
		return w.Flush()
	}

	return cmd
}
