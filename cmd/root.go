package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/term"
)

// ─────────────────────────────────────────────────────────────────────────────
// Root
// ─────────────────────────────────────────────────────────────────────────────

var rootCmd = &cobra.Command{
	Use:   "afribase",
	Short: "Afribase CLI — manage your projects from the terminal",
	Long: `
 █████╗ ███████╗██████╗ ██╗██████╗  █████╗ ███████╗███████╗
██╔══██╗██╔════╝██╔══██╗██║██╔══██╗██╔══██╗██╔════╝██╔════╝
███████║█████╗  ██████╔╝██║██████╔╝███████║███████╗█████╗  
██╔══██║██╔══╝  ██╔══██╗██║██╔══██╗██╔══██║╚════██║██╔══╝  
██║  ██║██║     ██║  ██║██║██████╔╝██║  ██║███████║███████╗
╚═╝  ╚═╝╚═╝     ╚═╝  ╚═╝╚═╝╚═════╝ ╚═╝  ╚═╝╚══════╝╚══════╝

The official CLI for Afribase — Africa's Backend-as-a-Service platform.

Get started:
  afribase login
  afribase projects list
  afribase db push --project my-project`,
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.AddCommand(loginCmd)
	rootCmd.AddCommand(logoutCmd)
	rootCmd.AddCommand(projectsCmd)
	rootCmd.AddCommand(dbCmd)
	rootCmd.AddCommand(migrationCmd)
	rootCmd.AddCommand(functionsCmd)
	rootCmd.AddCommand(envCmd)
	rootCmd.AddCommand(jobsCmd)
	rootCmd.AddCommand(linkCmd)
	rootCmd.AddCommand(aiCmd)
	rootCmd.AddCommand(appsCmd)
	rootCmd.AddCommand(versionCmd)
}

// Set by the linker at release time; see .goreleaser.yaml.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the CLI version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("afribase %s (%s, built %s)\n", version, commit, date)
	},
}

func initConfig() {
	home, _ := os.UserHomeDir()
	viper.SetConfigFile(filepath.Join(home, ".afribase", "config.yaml"))

	// AFRIBASE_ACCESS_TOKEN, AFRIBASE_API_URL, AFRIBASE_PROJECT_ID. Without the
	// prefix, AutomaticEnv would look for a bare ACCESS_TOKEN, which is both
	// unlikely to be set and dangerous to pick up if it were.
	viper.SetEnvPrefix("AFRIBASE")
	viper.AutomaticEnv()

	_ = viper.ReadInConfig()
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

func apiURL() string {
	if u := viper.GetString("api_url"); u != "" {
		return strings.TrimRight(u, "/")
	}
	return "https://api.useafribase.app"
}

func token() string {
	return viper.GetString("access_token")
}

// httpClient has an explicit timeout: http.DefaultClient has none, so a hung
// API would hang the CLI forever with no way out but Ctrl-C.
var httpClient = &http.Client{Timeout: 60 * time.Second}

// doRequest returns the decoded body as `any` rather than a map. Several
// endpoints - listApps among them - return a bare JSON array, and decoding
// those into map[string]interface{} fails silently and prints "null".
func doRequest(method, path string, body interface{}) (interface{}, error) {
	var buf io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		buf = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, apiURL()+path, buf)
	if err != nil {
		return nil, err
	}
	if t := token(); t != "" {
		req.Header.Set("Authorization", "Bearer "+t)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("not authenticated - run 'afribase login'")
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, apiError(respBytes))
	}
	if len(bytes.TrimSpace(respBytes)) == 0 {
		return nil, nil
	}

	var result interface{}
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, fmt.Errorf("could not decode response: %w", err)
	}
	return result, nil
}

// apiError digs the "error" field out of the standard error body so the user
// sees the message rather than the whole JSON envelope.
func apiError(body []byte) string {
	var envelope struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &envelope) == nil && envelope.Error != "" {
		return envelope.Error
	}
	return strings.TrimSpace(string(body))
}

// projectFlag registers --project on a command. It is deliberately not marked
// required: an unset flag falls back to the project set by `afribase link`.
func projectFlag(cmds ...*cobra.Command) {
	for _, c := range cmds {
		c.Flags().String("project", "", "Project ID or slug (defaults to the linked project)")
	}
}

// asMap narrows a decoded body to an object, or nil if it was an array or null.
func asMap(v interface{}) map[string]interface{} {
	m, _ := v.(map[string]interface{})
	return m
}

func printJSON(v interface{}) {
	b, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(b))
}

// ─────────────────────────────────────────────────────────────────────────────
// auth: login / logout
// ─────────────────────────────────────────────────────────────────────────────

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate with your Afribase account",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Print("Email: ")
		var email string
		fmt.Scanln(&email)

		fmt.Print("Password: ")
		pwd, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			return err
		}

		// Auth is handled by GoTrue, not the orchestrator stub endpoint.
		result, err := doRequest("POST", "/auth/token?grant_type=password", map[string]string{
			"email":    email,
			"password": string(pwd),
		})
		if err != nil {
			return fmt.Errorf("login failed: %w", err)
		}

		accessToken, _ := asMap(result)["access_token"].(string)
		if accessToken == "" {
			return fmt.Errorf("no access_token in response")
		}

		// Merge rather than overwrite: writing the file by hand here used to
		// drop project_id, silently unlinking the project on every login.
		if err := saveConfig(map[string]string{"access_token": accessToken}); err != nil {
			return err
		}

		home, _ := os.UserHomeDir()
		fmt.Printf("✅ Logged in. Credentials saved to %s\n", filepath.Join(home, ".afribase", "config.yaml"))
		return nil
	},
}

var logoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Log out and remove saved credentials",
	RunE: func(cmd *cobra.Command, args []string) error {
		home, _ := os.UserHomeDir()
		cfgFile := filepath.Join(home, ".afribase", "config.yaml")
		_ = os.Remove(cfgFile)
		fmt.Println("✅ Logged out. Credentials removed.")
		return nil
	},
}

// ─────────────────────────────────────────────────────────────────────────────
// projects: list / create / delete
// ─────────────────────────────────────────────────────────────────────────────

var projectsCmd = &cobra.Command{
	Use:   "projects",
	Short: "Manage your Afribase projects",
}

func init() {
	projectsCmd.AddCommand(projectsListCmd)
	projectsCmd.AddCommand(projectsCreateCmd)
	projectsCmd.AddCommand(projectsDeleteCmd)
	projectsDeleteCmd.Flags().Bool("yes", false, "Skip the confirmation prompt")
	projectsCreateCmd.Flags().String("name", "", "Project name (required)")
	projectsCreateCmd.Flags().String("region", "lagos-01", "Region")
	projectsCreateCmd.Flags().String("db-password", "", "Database password (required)")
	_ = projectsCreateCmd.MarkFlagRequired("name")
	_ = projectsCreateCmd.MarkFlagRequired("db-password")
}

var projectsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all your projects",
	RunE: func(cmd *cobra.Command, args []string) error {
		result, err := doRequest("GET", "/api/projects", nil)
		if err != nil {
			return err
		}
		printJSON(result)
		return nil
	},
}

var projectsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new project",
	RunE: func(cmd *cobra.Command, args []string) error {
		name, _ := cmd.Flags().GetString("name")
		region, _ := cmd.Flags().GetString("region")
		dbPass, _ := cmd.Flags().GetString("db-password")

		result, err := doRequest("POST", "/api/projects", map[string]interface{}{
			"name":             name,
			"region":           region,
			"databasePassword": dbPass,
		})
		if err != nil {
			return err
		}
		printJSON(result)
		return nil
	},
}

var projectsDeleteCmd = &cobra.Command{
	Use:   "delete <projectId>",
	Short: "Delete a project and everything in it",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		projectID := args[0]

		// Deleting a project destroys its database. A typo here is not
		// recoverable, so the name has to be typed back.
		if yes, _ := cmd.Flags().GetBool("yes"); !yes {
			fmt.Printf("This permanently deletes project %s, its database and its storage.\n", projectID)
			fmt.Print("Type the project id to confirm: ")
			var confirm string
			fmt.Scanln(&confirm)
			if strings.TrimSpace(confirm) != projectID {
				return fmt.Errorf("aborted")
			}
		}

		if _, err := doRequest("DELETE", "/api/projects/"+projectID, nil); err != nil {
			return err
		}
		fmt.Printf("✅ Project %s deleted.\n", projectID)
		return nil
	},
}

// ─────────────────────────────────────────────────────────────────────────────
// db: push / migrations
// ─────────────────────────────────────────────────────────────────────────────

var dbCmd = &cobra.Command{
	Use:   "db",
	Short: "Database operations for a project",
}

func init() {
	dbCmd.AddCommand(dbMigrationsListCmd)
	dbCmd.AddCommand(dbMigrationsCreateCmd)
	dbCmd.AddCommand(dbMigrationsRollbackCmd)

	projectFlag(dbMigrationsListCmd, dbMigrationsCreateCmd, dbMigrationsRollbackCmd)
	dbMigrationsCreateCmd.Flags().String("name", "", "Migration name (required)")
	dbMigrationsCreateCmd.Flags().String("sql", "", "SQL statement to run")
	dbMigrationsCreateCmd.Flags().String("file", "", "Path to .sql file")
	dbMigrationsCreateCmd.Flags().String("down-sql", "", "SQL that undoes this migration")
	dbMigrationsCreateCmd.Flags().String("down-file", "", "Path to a .sql file that undoes this migration")
	_ = dbMigrationsCreateCmd.MarkFlagRequired("name")
}

var dbMigrationsListCmd = &cobra.Command{
	Use:   "migrations",
	Short: "List the migrations the project has applied (see also: afribase migration list)",
	RunE: func(cmd *cobra.Command, args []string) error {
		projectID, perr := resolveProject(cmd)
		if perr != nil {
			return perr
		}
		result, err := doRequest("GET", fmt.Sprintf("/api/projects/%s/database/migrations", projectID), nil)
		if err != nil {
			return err
		}
		printJSON(result)
		return nil
	},
}

var dbMigrationsCreateCmd = &cobra.Command{
	Use:   "apply",
	Short: "Apply one ad-hoc migration from --sql or --file, without a migration file",
	RunE: func(cmd *cobra.Command, args []string) error {
		projectID, perr := resolveProject(cmd)
		if perr != nil {
			return perr
		}
		name, _ := cmd.Flags().GetString("name")
		sql, _ := cmd.Flags().GetString("sql")
		file, _ := cmd.Flags().GetString("file")

		if sql == "" && file != "" {
			b, err := os.ReadFile(file)
			if err != nil {
				return fmt.Errorf("could not read file: %w", err)
			}
			sql = string(b)
		}
		if sql == "" {
			return fmt.Errorf("provide --sql or --file")
		}

		downSQL, _ := cmd.Flags().GetString("down-sql")
		downFile, _ := cmd.Flags().GetString("down-file")
		if downSQL == "" && downFile != "" {
			b, err := os.ReadFile(downFile)
			if err != nil {
				return fmt.Errorf("could not read down file: %w", err)
			}
			downSQL = string(b)
		}

		result, err := doRequest("POST", fmt.Sprintf("/api/projects/%s/database/migrations", projectID), map[string]string{
			"name":    name,
			"sql":     sql,
			"downSql": downSQL,
		})
		if err != nil {
			return err
		}
		printJSON(result)
		fmt.Println("✅ Migration applied successfully.")
		if downSQL == "" {
			// Said now, while there is still a chance to write one, rather than
			// at the moment somebody needs to undo this and cannot.
			fmt.Println("⚠️  No down SQL given, so this migration cannot be rolled back.")
			fmt.Println("    Pass --down-sql or --down-file to make `afribase db rollback` work.")
		}
		return nil
	},
}

var dbMigrationsRollbackCmd = &cobra.Command{
	Use:   "rollback",
	Short: "Rollback the latest migration",
	RunE: func(cmd *cobra.Command, args []string) error {
		projectID, perr := resolveProject(cmd)
		if perr != nil {
			return perr
		}
		result, err := doRequest("POST", fmt.Sprintf("/api/projects/%s/database/migrations/rollback", projectID), map[string]string{})
		if err != nil {
			// The server refuses when a migration has no down SQL. That message
			// is the useful one, so it is passed through rather than replaced.
			return err
		}
		printJSON(result)
		fmt.Println("✅ Rolled back: the down SQL ran against your database.")
		return nil
	},
}

// ─────────────────────────────────────────────────────────────────────────────
// functions: list / deploy / invoke / delete
// ─────────────────────────────────────────────────────────────────────────────

var functionsCmd = &cobra.Command{
	Use:   "functions",
	Short: "Manage Edge Functions for a project",
}

func init() {
	functionsCmd.AddCommand(functionsListCmd)
	functionsCmd.AddCommand(functionsDeployCmd)
	functionsCmd.AddCommand(functionsDeleteCmd)

	projectFlag(functionsListCmd, functionsDeployCmd, functionsDeleteCmd)
	functionsDeployCmd.Flags().String("name", "", "Function name (required)")
	functionsDeployCmd.Flags().String("entry", "index.ts", "Entrypoint file")
	_ = functionsDeployCmd.MarkFlagRequired("name")

	functionsDeleteCmd.Flags().String("name", "", "Function name (required)")
	_ = functionsDeleteCmd.MarkFlagRequired("name")
}

var functionsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all edge functions",
	RunE: func(cmd *cobra.Command, args []string) error {
		projectID, perr := resolveProject(cmd)
		if perr != nil {
			return perr
		}
		result, err := doRequest("GET", fmt.Sprintf("/api/projects/%s/functions", projectID), nil)
		if err != nil {
			return err
		}
		printJSON(result)
		return nil
	},
}

var functionsDeployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "Deploy an edge function (reads local file)",
	RunE: func(cmd *cobra.Command, args []string) error {
		projectID, perr := resolveProject(cmd)
		if perr != nil {
			return perr
		}
		name, _ := cmd.Flags().GetString("name")
		entry, _ := cmd.Flags().GetString("entry")

		code, err := os.ReadFile(entry)
		if err != nil {
			return fmt.Errorf("could not read entrypoint file %q: %w", entry, err)
		}

		result, err := doRequest("POST", fmt.Sprintf("/api/projects/%s/functions", projectID), map[string]string{
			"name": name,
			"code": string(code),
		})
		if err != nil {
			return err
		}
		printJSON(result)
		fmt.Printf("✅ Function '%s' deployed.\n", name)
		return nil
	},
}

var functionsDeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete an edge function",
	RunE: func(cmd *cobra.Command, args []string) error {
		projectID, perr := resolveProject(cmd)
		if perr != nil {
			return perr
		}
		name, _ := cmd.Flags().GetString("name")

		_, err := doRequest("DELETE", fmt.Sprintf("/api/projects/%s/functions/%s", projectID, name), nil)
		if err != nil {
			return err
		}
		fmt.Printf("✅ Function '%s' deleted.\n", name)
		return nil
	},
}

// ─────────────────────────────────────────────────────────────────────────────
// env: list / set / delete (Env Config Store)
// ─────────────────────────────────────────────────────────────────────────────

var envCmd = &cobra.Command{
	Use:   "env",
	Short: "Manage project env config (non-secret key-value store)",
}

func init() {
	envCmd.AddCommand(envListCmd)
	envCmd.AddCommand(envSetCmd)
	envCmd.AddCommand(envDeleteCmd)

	projectFlag(envListCmd, envSetCmd, envDeleteCmd)
	envSetCmd.Flags().String("key", "", "Config key (required)")
	envSetCmd.Flags().String("value", "", "Config value (required)")
	envSetCmd.Flags().String("description", "", "Optional description")
	_ = envSetCmd.MarkFlagRequired("key")
	_ = envSetCmd.MarkFlagRequired("value")

	envDeleteCmd.Flags().String("key", "", "Config key to delete (required)")
	_ = envDeleteCmd.MarkFlagRequired("key")
}

var envListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all env config keys",
	RunE: func(cmd *cobra.Command, args []string) error {
		projectID, perr := resolveProject(cmd)
		if perr != nil {
			return perr
		}
		result, err := doRequest("GET", fmt.Sprintf("/api/projects/%s/config", projectID), nil)
		if err != nil {
			return err
		}
		printJSON(result)
		return nil
	},
}

var envSetCmd = &cobra.Command{
	Use:   "set",
	Short: "Set (create or update) an env config key",
	RunE: func(cmd *cobra.Command, args []string) error {
		projectID, perr := resolveProject(cmd)
		if perr != nil {
			return perr
		}
		key, _ := cmd.Flags().GetString("key")
		value, _ := cmd.Flags().GetString("value")
		desc, _ := cmd.Flags().GetString("description")

		result, err := doRequest("PUT", fmt.Sprintf("/api/projects/%s/config", projectID), map[string]string{
			"key":         key,
			"value":       value,
			"description": desc,
		})
		if err != nil {
			return err
		}
		printJSON(result)
		fmt.Printf("✅ Config key '%s' set.\n", key)
		return nil
	},
}

var envDeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete an env config key",
	RunE: func(cmd *cobra.Command, args []string) error {
		projectID, perr := resolveProject(cmd)
		if perr != nil {
			return perr
		}
		key, _ := cmd.Flags().GetString("key")

		_, err := doRequest("DELETE", fmt.Sprintf("/api/projects/%s/config/%s", projectID, key), nil)
		if err != nil {
			return err
		}
		fmt.Printf("✅ Config key '%s' deleted.\n", key)
		return nil
	},
}

// ─────────────────────────────────────────────────────────────────────────────
// jobs: list / enqueue / retry / cancel
// ─────────────────────────────────────────────────────────────────────────────

var jobsCmd = &cobra.Command{
	Use:   "jobs",
	Short: "Manage background jobs for a project",
}

func init() {
	jobsCmd.AddCommand(jobsListCmd)
	jobsCmd.AddCommand(jobsEnqueueCmd)
	jobsCmd.AddCommand(jobsRetryCmd)
	jobsCmd.AddCommand(jobsCancelCmd)

	projectFlag(jobsListCmd, jobsEnqueueCmd, jobsRetryCmd, jobsCancelCmd)

	jobsListCmd.Flags().String("queue", "", "Filter by queue name")
	jobsListCmd.Flags().String("status", "", "Filter by status")

	jobsEnqueueCmd.Flags().String("name", "", "Job name (required)")
	jobsEnqueueCmd.Flags().String("queue", "default", "Queue name")
	jobsEnqueueCmd.Flags().String("payload", "{}", "JSON payload")
	_ = jobsEnqueueCmd.MarkFlagRequired("name")

	for _, c := range []*cobra.Command{jobsRetryCmd, jobsCancelCmd} {
		c.Flags().String("job-id", "", "Job ID (required)")
		_ = c.MarkFlagRequired("job-id")
	}
}

var jobsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List jobs in a project queue",
	RunE: func(cmd *cobra.Command, args []string) error {
		projectID, perr := resolveProject(cmd)
		if perr != nil {
			return perr
		}
		queue, _ := cmd.Flags().GetString("queue")
		status, _ := cmd.Flags().GetString("status")

		path := fmt.Sprintf("/api/projects/%s/jobs", projectID)
		params := []string{}
		if queue != "" {
			params = append(params, "queue="+queue)
		}
		if status != "" {
			params = append(params, "status="+status)
		}
		if len(params) > 0 {
			path += "?" + strings.Join(params, "&")
		}

		result, err := doRequest("GET", path, nil)
		if err != nil {
			return err
		}
		printJSON(result)
		return nil
	},
}

var jobsEnqueueCmd = &cobra.Command{
	Use:   "enqueue",
	Short: "Enqueue a new background job",
	RunE: func(cmd *cobra.Command, args []string) error {
		projectID, perr := resolveProject(cmd)
		if perr != nil {
			return perr
		}
		name, _ := cmd.Flags().GetString("name")
		queue, _ := cmd.Flags().GetString("queue")
		payloadStr, _ := cmd.Flags().GetString("payload")

		var payload map[string]interface{}
		_ = json.Unmarshal([]byte(payloadStr), &payload)

		result, err := doRequest("POST", fmt.Sprintf("/api/projects/%s/jobs", projectID), map[string]interface{}{
			"name":    name,
			"queue":   queue,
			"payload": payload,
		})
		if err != nil {
			return err
		}
		printJSON(result)
		fmt.Println("✅ Job enqueued.")
		return nil
	},
}

var jobsRetryCmd = &cobra.Command{
	Use:   "retry",
	Short: "Retry a failed or dead job",
	RunE: func(cmd *cobra.Command, args []string) error {
		projectID, perr := resolveProject(cmd)
		if perr != nil {
			return perr
		}
		jobID, _ := cmd.Flags().GetString("job-id")

		result, err := doRequest("POST", fmt.Sprintf("/api/projects/%s/jobs/%s/retry", projectID, jobID), nil)
		if err != nil {
			return err
		}
		printJSON(result)
		fmt.Println("✅ Job queued for retry.")
		return nil
	},
}

var jobsCancelCmd = &cobra.Command{
	Use:   "cancel",
	Short: "Cancel a pending job",
	RunE: func(cmd *cobra.Command, args []string) error {
		projectID, perr := resolveProject(cmd)
		if perr != nil {
			return perr
		}
		jobID, _ := cmd.Flags().GetString("job-id")

		_, err := doRequest("DELETE", fmt.Sprintf("/api/projects/%s/jobs/%s", projectID, jobID), nil)
		if err != nil {
			return err
		}
		fmt.Println("✅ Job cancelled.")
		return nil
	},
}
