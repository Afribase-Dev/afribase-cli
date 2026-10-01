package cmd

import (
	"encoding/json"
	"fmt"

	"strings"
	"time"

	"github.com/spf13/cobra"
)

// ─────────────────────────────────────────────────────────────────────────────
// apps: the deploy-from-git plane
//
// Mirrors /api/projects/:id/apps. The one command that does more than call an
// endpoint is `deploy --wait`, which polls the deploy until it leaves the
// in-flight states, because "did my push actually go live" is the question the
// terminal is open to answer.
// ─────────────────────────────────────────────────────────────────────────────

var appsCmd = &cobra.Command{
	Use:   "apps",
	Short: "Deploy and manage apps built from a git repository",
}

func init() {
	appsCmd.AddCommand(appsListCmd, appsCreateCmd, appsDeployCmd, appsLogsCmd,
		appsDeploysCmd, appsRollbackCmd, appsEnvCmd, appsDeleteCmd)

	projectFlag(appsListCmd, appsCreateCmd, appsDeployCmd, appsLogsCmd,
		appsDeploysCmd, appsRollbackCmd, appsDeleteCmd)

	appsCreateCmd.Flags().String("name", "", "App name (required)")
	appsCreateCmd.Flags().String("repo", "", "Repository URL, e.g. https://github.com/me/api (required for git apps)")
	appsCreateCmd.Flags().String("branch", "main", "Branch to deploy")
	appsCreateCmd.Flags().String("root-dir", "", "Build context within the repository")
	appsCreateCmd.Flags().String("dockerfile", "", "Path to a Dockerfile, relative to the root directory")
	appsCreateCmd.Flags().String("runtime", "", "docker, node, python, go, ruby, flutter, dart, static and the rest. Detected when omitted")
	appsCreateCmd.Flags().String("build-command", "", "Override the detected build command")
	appsCreateCmd.Flags().String("start-command", "", "Override the detected start command")
	appsCreateCmd.Flags().String("image", "", "Deploy a prebuilt image instead of building from a repository")
	appsCreateCmd.Flags().Int("port", 0, "Port your app listens on")
	appsCreateCmd.Flags().String("health-check", "", "Health check path, e.g. /healthz")
	appsCreateCmd.Flags().Bool("no-auto-deploy", false, "Do not deploy automatically on push")
	_ = appsCreateCmd.MarkFlagRequired("name")

	appsDeployCmd.Flags().String("app", "", "App ID or slug (required)")
	appsDeployCmd.Flags().String("commit", "", "Commit SHA to deploy (defaults to the branch head)")
	appsDeployCmd.Flags().Bool("clear-cache", false, "Build without the layer cache")
	appsDeployCmd.Flags().Bool("wait", false, "Wait for the deploy to finish and exit non-zero if it fails")
	_ = appsDeployCmd.MarkFlagRequired("app")

	appsLogsCmd.Flags().String("app", "", "App ID or slug (required)")
	appsLogsCmd.Flags().Int("tail", 100, "Number of lines to return")
	_ = appsLogsCmd.MarkFlagRequired("app")

	appsDeploysCmd.Flags().String("app", "", "App ID or slug (required)")
	_ = appsDeploysCmd.MarkFlagRequired("app")

	appsRollbackCmd.Flags().String("app", "", "App ID or slug (required)")
	appsRollbackCmd.Flags().String("deploy", "", "Deploy ID to roll back to (required)")
	_ = appsRollbackCmd.MarkFlagRequired("app")
	_ = appsRollbackCmd.MarkFlagRequired("deploy")

	appsDeleteCmd.Flags().String("app", "", "App ID or slug (required)")
	appsDeleteCmd.Flags().Bool("yes", false, "Skip the confirmation prompt")
	_ = appsDeleteCmd.MarkFlagRequired("app")

	// env sub-tree
	appsEnvCmd.AddCommand(appsEnvListCmd, appsEnvSetCmd, appsEnvUnsetCmd)
	projectFlag(appsEnvListCmd, appsEnvSetCmd, appsEnvUnsetCmd)
	for _, c := range []*cobra.Command{appsEnvListCmd, appsEnvSetCmd, appsEnvUnsetCmd} {
		c.Flags().String("app", "", "App ID or slug (required)")
		_ = c.MarkFlagRequired("app")
	}
	appsEnvSetCmd.Flags().String("key", "", "Variable name (required)")
	appsEnvSetCmd.Flags().String("value", "", "Value (required)")
	appsEnvSetCmd.Flags().Bool("secret", false, "Store as a secret, hidden from later reads")
	_ = appsEnvSetCmd.MarkFlagRequired("key")
	_ = appsEnvSetCmd.MarkFlagRequired("value")
	appsEnvUnsetCmd.Flags().String("key", "", "Variable name (required)")
	_ = appsEnvUnsetCmd.MarkFlagRequired("key")
}

// appPath builds the /api/projects/:id/apps/:appId prefix both ids are needed for.
func appPath(cmd *cobra.Command, suffix string) (string, error) {
	projectID, err := resolveProject(cmd)
	if err != nil {
		return "", err
	}
	appID, _ := cmd.Flags().GetString("app")
	return fmt.Sprintf("/api/projects/%s/apps/%s%s", projectID, appID, suffix), nil
}

var appsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List the apps in a project",
	RunE: func(cmd *cobra.Command, args []string) error {
		projectID, err := resolveProject(cmd)
		if err != nil {
			return err
		}
		result, err := doRequest("GET", fmt.Sprintf("/api/projects/%s/apps", projectID), nil)
		if err != nil {
			return err
		}
		printJSON(result)
		return nil
	},
}

var appsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create an app from a repository or a prebuilt image",
	RunE: func(cmd *cobra.Command, args []string) error {
		projectID, err := resolveProject(cmd)
		if err != nil {
			return err
		}

		name, _ := cmd.Flags().GetString("name")
		repo, _ := cmd.Flags().GetString("repo")
		image, _ := cmd.Flags().GetString("image")
		if repo == "" && image == "" {
			return fmt.Errorf("provide --repo to build from a repository, or --image to deploy a prebuilt image")
		}

		body := map[string]interface{}{"name": name}
		if image != "" {
			body["source"] = "image"
			body["imageRef"] = image
		} else {
			body["source"] = "git"
			body["repoUrl"] = repo
			branch, _ := cmd.Flags().GetString("branch")
			body["branch"] = branch
		}

		// Only send what was set: the server fills in the rest, and an empty
		// string would overwrite a sensible default with nothing.
		for flag, key := range map[string]string{
			"root-dir":      "rootDir",
			"dockerfile":    "dockerfilePath",
			"runtime":       "runtime",
			"build-command": "buildCommand",
			"start-command": "startCommand",
			"health-check":  "healthCheckPath",
		} {
			if v, _ := cmd.Flags().GetString(flag); v != "" {
				body[key] = v
			}
		}
		if port, _ := cmd.Flags().GetInt("port"); port > 0 {
			body["port"] = port
		}
		if noAuto, _ := cmd.Flags().GetBool("no-auto-deploy"); noAuto {
			body["autoDeploy"] = false
		}

		result, err := doRequest("POST", fmt.Sprintf("/api/projects/%s/apps", projectID), body)
		if err != nil {
			return err
		}
		printJSON(result)

		if slug, ok := asMap(result)["slug"].(string); ok {
			fmt.Printf("\n✅ App created. Deploy it with:\n   afribase apps deploy --app %s --wait\n", slug)
		}
		return nil
	},
}

var appsDeployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "Trigger a deploy",
	RunE: func(cmd *cobra.Command, args []string) error {
		path, err := appPath(cmd, "/deploys")
		if err != nil {
			return err
		}

		body := map[string]interface{}{}
		if sha, _ := cmd.Flags().GetString("commit"); sha != "" {
			body["commitSha"] = sha
		}
		if clear, _ := cmd.Flags().GetBool("clear-cache"); clear {
			body["clearCache"] = true
		}

		result, err := doRequest("POST", path, body)
		if err != nil {
			return err
		}

		deploy := asMap(result)
		deployID, _ := deploy["id"].(string)
		fmt.Printf("🚀 Deploy %s queued.\n", deployID)

		if wait, _ := cmd.Flags().GetBool("wait"); !wait || deployID == "" {
			return nil
		}
		return waitForDeploy(cmd, deployID)
	},
}

// Terminal states, matching models.DeployStatus exactly. "superseded" means a
// newer deploy overtook this one, which is not a failure but is still the end
// of this deploy's life, so waiting on it must stop rather than hang.
var (
	deploySucceeded = map[string]bool{"live": true}
	deployFailed    = map[string]bool{"failed": true, "canceled": true}
	deployStopped   = map[string]bool{"superseded": true}
)

// waitForDeploy polls until the deploy settles, then exits non-zero on failure
// so a CI step fails when the deploy does.
func waitForDeploy(cmd *cobra.Command, deployID string) error {
	path, err := appPath(cmd, "/deploys/"+deployID)
	if err != nil {
		return err
	}

	var last string
	deadline := time.Now().Add(30 * time.Minute)

	for time.Now().Before(deadline) {
		result, err := doRequest("GET", path, nil)
		if err != nil {
			return err
		}

		status, _ := asMap(result)["status"].(string)
		if status != last && status != "" {
			fmt.Printf("   %s\n", status)
			last = status
		}

		switch {
		case deploySucceeded[strings.ToLower(status)]:
			fmt.Println("✅ Deploy live.")
			return nil
		case deployFailed[strings.ToLower(status)]:
			return fmt.Errorf("deploy %s %s", deployID, status)
		case deployStopped[strings.ToLower(status)]:
			fmt.Println("⚠️  Superseded by a newer deploy.")
			return nil
		}

		time.Sleep(5 * time.Second)
	}

	return fmt.Errorf("timed out waiting for deploy %s; it may still be running", deployID)
}

var appsDeploysCmd = &cobra.Command{
	Use:   "deploys",
	Short: "List an app's deploy history",
	RunE: func(cmd *cobra.Command, args []string) error {
		path, err := appPath(cmd, "/deploys")
		if err != nil {
			return err
		}
		result, err := doRequest("GET", path, nil)
		if err != nil {
			return err
		}
		printJSON(result)
		return nil
	},
}

var appsRollbackCmd = &cobra.Command{
	Use:   "rollback",
	Short: "Roll back to a previous deploy",
	RunE: func(cmd *cobra.Command, args []string) error {
		deployID, _ := cmd.Flags().GetString("deploy")
		path, err := appPath(cmd, "/deploys/"+deployID+"/rollback")
		if err != nil {
			return err
		}
		result, err := doRequest("POST", path, nil)
		if err != nil {
			return err
		}
		printJSON(result)
		fmt.Println("✅ Rollback started.")
		return nil
	},
}

var appsLogsCmd = &cobra.Command{
	Use:   "logs",
	Short: "Print an app's recent logs",
	RunE: func(cmd *cobra.Command, args []string) error {
		tail, _ := cmd.Flags().GetInt("tail")
		path, err := appPath(cmd, fmt.Sprintf("/logs?tail=%d", tail))
		if err != nil {
			return err
		}
		result, err := doRequest("GET", path, nil)
		if err != nil {
			return err
		}

		// Logs come back as an array of lines. Print them as lines, not as
		// JSON, since that is the only reason anyone runs this command.
		if lines, ok := result.([]interface{}); ok {
			for _, line := range lines {
				if s, ok := line.(string); ok {
					fmt.Println(s)
					continue
				}
				b, _ := json.Marshal(line)
				fmt.Println(string(b))
			}
			return nil
		}
		printJSON(result)
		return nil
	},
}

var appsEnvCmd = &cobra.Command{
	Use:   "env",
	Short: "Manage an app's environment variables",
}

var appsEnvListCmd = &cobra.Command{
	Use:   "list",
	Short: "List an app's environment variables",
	RunE: func(cmd *cobra.Command, args []string) error {
		path, err := appPath(cmd, "/env")
		if err != nil {
			return err
		}
		result, err := doRequest("GET", path, nil)
		if err != nil {
			return err
		}
		printJSON(result)
		return nil
	},
}

var appsEnvSetCmd = &cobra.Command{
	Use:   "set",
	Short: "Set an environment variable",
	RunE: func(cmd *cobra.Command, args []string) error {
		path, err := appPath(cmd, "/env")
		if err != nil {
			return err
		}
		key, _ := cmd.Flags().GetString("key")
		value, _ := cmd.Flags().GetString("value")
		secret, _ := cmd.Flags().GetBool("secret")

		if _, err := doRequest("PUT", path, map[string]interface{}{
			"key": key, "value": value, "isSecret": secret,
		}); err != nil {
			return err
		}
		fmt.Printf("✅ %s set. It applies on the next deploy.\n", key)
		return nil
	},
}

var appsEnvUnsetCmd = &cobra.Command{
	Use:   "unset",
	Short: "Remove an environment variable",
	RunE: func(cmd *cobra.Command, args []string) error {
		key, _ := cmd.Flags().GetString("key")
		path, err := appPath(cmd, "/env/"+key)
		if err != nil {
			return err
		}
		if _, err := doRequest("DELETE", path, nil); err != nil {
			return err
		}
		fmt.Printf("✅ %s removed. It applies on the next deploy.\n", key)
		return nil
	},
}

var appsDeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete an app and stop its containers",
	RunE: func(cmd *cobra.Command, args []string) error {
		appID, _ := cmd.Flags().GetString("app")
		path, err := appPath(cmd, "")
		if err != nil {
			return err
		}

		if yes, _ := cmd.Flags().GetBool("yes"); !yes {
			fmt.Printf("This deletes app %s and stops it serving traffic.\n", appID)
			fmt.Print("Type the app id to confirm: ")
			var confirm string
			fmt.Scanln(&confirm)
			if strings.TrimSpace(confirm) != appID {
				return fmt.Errorf("aborted")
			}
		}

		if _, err := doRequest("DELETE", path, nil); err != nil {
			return err
		}
		fmt.Printf("✅ App %s deleted.\n", appID)
		return nil
	},
}
