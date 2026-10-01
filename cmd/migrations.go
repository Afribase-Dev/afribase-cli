package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// ─────────────────────────────────────────────────────────────────────────────
// File-based migrations
//
// Migrations live as timestamped .sql files in afribase/migrations/, committed
// to git. The server keeps a record of which versions it has applied. Those are
// two separate systems and the whole job of these commands is to show where
// they agree and to move one towards the other.
//
// Before this, `db push --name --sql` sent one statement to the server, which
// ran it immediately. That meant every schema change was a direct change to the
// remote database — there was no file to review, no order to replay, and no way
// to build the same schema anywhere else. A branch built from migrations needs
// files to build from, which is why this comes first.
// ─────────────────────────────────────────────────────────────────────────────

const migrationsDir = "afribase/migrations"
const seedFile = "afribase/seed.sql"

// migrationFile is one file on disk. Version is the leading timestamp, which is
// what the server records and what orders the sequence.
type migrationFile struct {
	Version string
	Name    string
	Path    string
}

// fileNamePattern matches 20260904120000_create_employees.sql. The timestamp is
// fixed-width so lexical order is chronological order — which is what lets the
// sequence be sorted as strings without parsing every name.
var fileNamePattern = regexp.MustCompile(`^(\d{14})_(.+)\.sql$`)

func localMigrations() ([]migrationFile, error) {
	entries, err := os.ReadDir(migrationsDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var out []migrationFile
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		// The down file sits beside its migration and matches the same pattern,
		// because `.+` happily swallows the ".down". Left in, `db push` would
		// apply it as a migration of its own and undo the change it exists to
		// reverse. Checked before the pattern, not after.
		if strings.HasSuffix(e.Name(), ".down.sql") {
			continue
		}
		m := fileNamePattern.FindStringSubmatch(e.Name())
		if m == nil {
			// Not an error. A README or an editor's backup file living beside
			// the migrations should not stop a push.
			continue
		}
		out = append(out, migrationFile{
			Version: m[1],
			Name:    m[2],
			Path:    filepath.Join(migrationsDir, e.Name()),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out, nil
}

// remoteMigration is what the server reports about one version.
type remoteMigration struct {
	Version string `json:"version"`
	Name    string `json:"name"`
	Status  string `json:"status"`
}

func remoteMigrations(projectID string) (map[string]remoteMigration, error) {
	result, err := doRequest("GET", fmt.Sprintf("/api/projects/%s/database/migrations", projectID), nil)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	var list []remoteMigration
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("could not read the migration list: %w", err)
	}

	out := map[string]remoteMigration{}
	for _, m := range list {
		out[m.Version] = m
	}
	return out, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// migration new
// ─────────────────────────────────────────────────────────────────────────────

var migrationNewCmd = &cobra.Command{
	Use:   "new <name>",
	Short: "Create a new migration file",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := slugify(args[0])
		if name == "" {
			return fmt.Errorf("give the migration a name, e.g. create_employees_table")
		}
		if err := os.MkdirAll(migrationsDir, 0o755); err != nil {
			return err
		}

		version := time.Now().UTC().Format("20060102150405")
		path := filepath.Join(migrationsDir, fmt.Sprintf("%s_%s.sql", version, name))

		body := fmt.Sprintf(`-- %s
--
-- Write the change below. Everything in this file runs in one transaction:
-- either every statement lands or none of them do.
--
-- To make this reversible, put the undo in a matching .down.sql file:
--   %s_%s.down.sql
-- Without one, `+"`afribase db rollback`"+` refuses rather than reporting a
-- rollback that did not happen.

`, strings.ReplaceAll(name, "_", " "), version, name)

		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			return err
		}
		fmt.Printf("Created %s\n", path)
		return nil
	},
}

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(s, "_")
	return strings.Trim(s, "_")
}

// ─────────────────────────────────────────────────────────────────────────────
// migration list
// ─────────────────────────────────────────────────────────────────────────────

var migrationListCmd = &cobra.Command{
	Use:   "list",
	Short: "Show local migration files against what the project has applied",
	RunE: func(cmd *cobra.Command, args []string) error {
		projectID, err := resolveProject(cmd)
		if err != nil {
			return err
		}
		local, err := localMigrations()
		if err != nil {
			return err
		}
		remote, err := remoteMigrations(projectID)
		if err != nil {
			return err
		}

		// The union, so a version that exists only on the server is visible
		// too. That is the case that matters: somebody applied something this
		// checkout has never seen, and a list of local files alone would show
		// everything as fine.
		versions := map[string]bool{}
		names := map[string]string{}
		for _, f := range local {
			versions[f.Version] = true
			names[f.Version] = f.Name
		}
		for v, m := range remote {
			versions[v] = true
			if names[v] == "" {
				names[v] = m.Name
			}
		}
		if len(versions) == 0 {
			fmt.Println("No migrations, locally or on the project.")
			fmt.Println("Create one with: afribase migration new <name>")
			return nil
		}

		ordered := make([]string, 0, len(versions))
		for v := range versions {
			ordered = append(ordered, v)
		}
		sort.Strings(ordered)

		localSet := map[string]bool{}
		for _, f := range local {
			localSet[f.Version] = true
		}

		fmt.Printf("%-16s  %-30s  %-8s  %s\n", "VERSION", "NAME", "LOCAL", "PROJECT")
		var pending, orphans int
		for _, v := range ordered {
			r, onRemote := remote[v]
			status := "—"
			if onRemote {
				status = r.Status
			}
			localMark := "yes"
			if !localSet[v] {
				localMark = "no"
				orphans++
			}
			if localSet[v] && (!onRemote || r.Status != "applied") {
				pending++
			}
			fmt.Printf("%-16s  %-30s  %-8s  %s\n", v, truncate(names[v], 30), localMark, status)
		}

		fmt.Println()
		if pending > 0 {
			fmt.Printf("%d migration(s) not yet applied. Run: afribase db push\n", pending)
		}
		if orphans > 0 {
			// The dangerous direction. Something ran against the database that
			// is not in this checkout, so `db push` cannot reproduce it and a
			// branch built from these files would come out different.
			fmt.Printf("%d version(s) applied to the project have no file here.\n", orphans)
			fmt.Println("   Someone applied a change this checkout does not have. Run `afribase db pull`")
			fmt.Println("   or fetch the missing files before pushing.")
		}
		if pending == 0 && orphans == 0 {
			fmt.Println("Up to date.")
		}
		return nil
	},
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// ─────────────────────────────────────────────────────────────────────────────
// migration repair
// ─────────────────────────────────────────────────────────────────────────────

var migrationRepairCmd = &cobra.Command{
	Use:   "repair <version>",
	Short: "Correct a migration's record without running any SQL",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		projectID, err := resolveProject(cmd)
		if err != nil {
			return err
		}
		status, _ := cmd.Flags().GetString("status")
		if status != "applied" && status != "reverted" {
			return fmt.Errorf("--status must be 'applied' or 'reverted'")
		}

		name := ""
		if local, _ := localMigrations(); local != nil {
			for _, f := range local {
				if f.Version == args[0] {
					name = f.Name
				}
			}
		}

		result, err := doRequest("POST",
			fmt.Sprintf("/api/projects/%s/database/migrations/repair", projectID),
			map[string]string{"version": args[0], "status": status, "name": name})
		if err != nil {
			return err
		}
		printJSON(result)
		// Said plainly, because this is the command people reach for when they
		// are already unsure what state things are in.
		fmt.Printf("Record updated. No SQL was run — the database is unchanged.\n")
		return nil
	},
}

// ─────────────────────────────────────────────────────────────────────────────
// db push / pull / seed
// ─────────────────────────────────────────────────────────────────────────────

var dbPushCmd = &cobra.Command{
	Use:   "push",
	Short: "Apply pending migration files to the project, in order",
	RunE: func(cmd *cobra.Command, args []string) error {
		projectID, err := resolveProject(cmd)
		if err != nil {
			return err
		}
		local, err := localMigrations()
		if err != nil {
			return err
		}
		if len(local) == 0 {
			return fmt.Errorf("no migration files in %s; create one with: afribase migration new <name>", migrationsDir)
		}
		remote, err := remoteMigrations(projectID)
		if err != nil {
			return err
		}

		dryRun, _ := cmd.Flags().GetBool("dry-run")

		var pending []migrationFile
		for _, f := range local {
			if r, ok := remote[f.Version]; ok && r.Status == "applied" {
				continue
			}
			pending = append(pending, f)
		}
		if len(pending) == 0 {
			fmt.Println("Up to date. Nothing to apply.")
			return nil
		}

		fmt.Printf("%d migration(s) to apply:\n", len(pending))
		for _, f := range pending {
			fmt.Printf("  %s  %s\n", f.Version, f.Name)
		}
		if dryRun {
			fmt.Println("\nDry run, nothing applied.")
			return nil
		}
		fmt.Println()

		// In order, one at a time, stopping at the first failure. Continuing
		// past a failed migration would apply later files onto a schema their
		// SQL no longer describes.
		for _, f := range pending {
			body, readErr := os.ReadFile(f.Path)
			if readErr != nil {
				return fmt.Errorf("could not read %s: %w", f.Path, readErr)
			}

			// The matching .down.sql, if the author wrote one.
			down := ""
			downPath := strings.TrimSuffix(f.Path, ".sql") + ".down.sql"
			if b, dErr := os.ReadFile(downPath); dErr == nil {
				down = string(b)
			}

			fmt.Printf("Applying %s %s ... ", f.Version, f.Name)
			_, applyErr := doRequest("POST",
				fmt.Sprintf("/api/projects/%s/database/migrations", projectID),
				map[string]string{
					"version": f.Version,
					"name":    f.Name,
					"sql":     string(body),
					"downSql": down,
				})
			if applyErr != nil {
				fmt.Println("failed")
				return fmt.Errorf("%s: %w\n\nLater migrations were not applied", f.Name, applyErr)
			}
			fmt.Println("done")
		}

		fmt.Printf("\nApplied %d migration(s).\n", len(pending))
		return nil
	},
}

var dbPullCmd = &cobra.Command{
	Use:   "pull",
	Short: "Write the project's current schema to a migration file",
	RunE: func(cmd *cobra.Command, args []string) error {
		projectID, err := resolveProject(cmd)
		if err != nil {
			return err
		}
		result, err := doRequest("GET", fmt.Sprintf("/api/projects/%s/database/schema/dump", projectID), nil)
		if err != nil {
			return err
		}
		m, ok := result.(map[string]interface{})
		if !ok {
			return fmt.Errorf("unexpected response from the schema dump")
		}
		sqlText, _ := m["sql"].(string)
		if strings.TrimSpace(sqlText) == "" {
			return fmt.Errorf("the schema dump came back empty")
		}

		if err := os.MkdirAll(migrationsDir, 0o755); err != nil {
			return err
		}
		name, _ := cmd.Flags().GetString("name")
		if name == "" {
			name = "remote_schema"
		}
		version := time.Now().UTC().Format("20060102150405")
		path := filepath.Join(migrationsDir, fmt.Sprintf("%s_%s.sql", version, slugify(name)))
		if err := os.WriteFile(path, []byte(sqlText), 0o644); err != nil {
			return err
		}

		fmt.Printf("Wrote %s\n\n", path)
		// Said every time, because pushing this file back at the database it
		// came from is the obvious next move and it would fail on every object
		// that already exists.
		fmt.Println("This file describes a schema the project already has, so do not push it there.")
		fmt.Printf("Record it as already applied:\n  afribase migration repair %s --status applied\n", version)
		return nil
	},
}

var dbSeedCmd = &cobra.Command{
	Use:   "seed",
	Short: "Run afribase/seed.sql against the project",
	RunE: func(cmd *cobra.Command, args []string) error {
		projectID, err := resolveProject(cmd)
		if err != nil {
			return err
		}
		body, err := os.ReadFile(seedFile)
		if err != nil {
			return fmt.Errorf("no %s to run: %w", seedFile, err)
		}

		// Sent through the SQL endpoint rather than recorded as a migration:
		// seed data is not schema, it is re-runnable, and putting it in the
		// migration history would make every branch replay it as a change.
		result, err := doRequest("POST", fmt.Sprintf("/api/projects/%s/query", projectID),
			map[string]string{"query": string(body)})
		if err != nil {
			return err
		}
		printJSON(result)
		fmt.Println("Seed applied.")
		return nil
	},
}

func init() {
	migrationCmd.AddCommand(migrationNewCmd, migrationListCmd, migrationRepairCmd)
	migrationRepairCmd.Flags().String("status", "", "applied or reverted (required)")
	projectFlag(migrationListCmd, migrationRepairCmd)

	dbCmd.AddCommand(dbPushCmd, dbPullCmd, dbSeedCmd)
	dbPushCmd.Flags().Bool("dry-run", false, "List what would be applied, without applying it")
	dbPullCmd.Flags().String("name", "remote_schema", "Name for the generated migration file")
	projectFlag(dbPushCmd, dbPullCmd, dbSeedCmd)
}

var migrationCmd = &cobra.Command{
	Use:   "migration",
	Short: "Create and track schema migrations as files",
}
