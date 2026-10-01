package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// resolveProject returns the project id from --project or the linked default.
func resolveProject(cmd *cobra.Command) (string, error) {
	if p, _ := cmd.Flags().GetString("project"); p != "" {
		return p, nil
	}
	if p := viper.GetString("project_id"); p != "" {
		return p, nil
	}
	return "", fmt.Errorf("no project selected — run 'afribase link <projectId>' or pass --project <id>")
}

// saveConfig rewrites ~/.afribase/config.yaml preserving token + api_url.
func saveConfig(extra map[string]string) error {
	home, _ := os.UserHomeDir()
	cfgDir := filepath.Join(home, ".afribase")
	_ = os.MkdirAll(cfgDir, 0700)
	cfgFile := filepath.Join(cfgDir, "config.yaml")

	vals := map[string]string{
		"access_token": viper.GetString("access_token"),
		"api_url":      apiURL(),
		"project_id":   viper.GetString("project_id"),
	}
	for k, v := range extra {
		vals[k] = v
	}

	var b strings.Builder
	for _, k := range []string{"access_token", "api_url", "project_id"} {
		if vals[k] != "" {
			b.WriteString(fmt.Sprintf("%s: %s\n", k, vals[k]))
		}
	}
	return os.WriteFile(cfgFile, []byte(b.String()), 0600)
}

// ── link ────────────────────────────────────────────────────────────────────

var linkCmd = &cobra.Command{
	Use:   "link <projectId>",
	Short: "Set the default project for subsequent commands",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := saveConfig(map[string]string{"project_id": args[0]}); err != nil {
			return err
		}
		fmt.Printf("✅ Linked to project %s\n", args[0])
		return nil
	},
}

// ── ai build ──────────────────────────────────────────────────────────────────

var aiCmd = &cobra.Command{
	Use:   "ai",
	Short: "AI-powered backend building",
}

var aiBuildCmd = &cobra.Command{
	Use:   "build \"<prompt>\"",
	Short: "Describe a change; Afribase plans tables, RLS, and edge functions",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		projectID, err := resolveProject(cmd)
		if err != nil {
			return err
		}
		prompt := strings.Join(args, " ")

		fmt.Println("⚡ Planning…")
		plan, err := doRequest("POST", "/api/projects/"+projectID+"/ai/build", map[string]string{"prompt": prompt})
		if err != nil {
			return err
		}

		summary, _ := asMap(plan)["summary"].(string)
		planID, _ := asMap(plan)["planId"].(string)
		diff, _ := asMap(plan)["diff"].([]interface{})
		rls, _ := asMap(plan)["rlsPolicies"].([]interface{})
		fns, _ := asMap(plan)["functions"].([]interface{})

		fmt.Printf("\n%s\n\n", summary)
		fmt.Printf("  Schema changes : %d\n", len(diff))
		fmt.Printf("  RLS policies   : %d\n", len(rls))
		fmt.Printf("  Edge functions : %d\n\n", len(fns))

		apply, _ := cmd.Flags().GetBool("apply")
		if !apply {
			fmt.Println("Preview only. Re-run with --apply to apply this plan.")
			return nil
		}

		fmt.Println("🚀 Applying…")
		res, err := doRequest("POST", "/api/projects/"+projectID+"/ai/build/apply", map[string]string{"planId": planID})
		if err != nil {
			return err
		}
		stmts, _ := asMap(res)["statementsRun"].(float64)
		deployed, _ := asMap(res)["functionsDeployed"].(float64)
		fmt.Printf("✅ Applied: %d statements, %d functions deployed.\n", int(stmts), int(deployed))
		return nil
	},
}

func init() {
	aiCmd.AddCommand(aiBuildCmd)
	aiBuildCmd.Flags().Bool("apply", false, "Apply the plan instead of previewing")
	aiBuildCmd.Flags().String("project", "", "Project ID (overrides the linked default)")
}
