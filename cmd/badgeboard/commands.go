package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/NotRealShanks/badgeboard/internal/cache"
	"github.com/NotRealShanks/badgeboard/internal/config"
	"github.com/NotRealShanks/badgeboard/internal/export"
	"github.com/NotRealShanks/badgeboard/internal/fetch"
	"github.com/NotRealShanks/badgeboard/internal/model"
	"github.com/NotRealShanks/badgeboard/internal/parse"
	"github.com/NotRealShanks/badgeboard/internal/ratelimit"
	"github.com/NotRealShanks/badgeboard/internal/roster"
	"github.com/NotRealShanks/badgeboard/internal/tracker"
	"github.com/NotRealShanks/badgeboard/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

// Exit codes per spec
const (
	ExitSuccess = 0 // All profiles evaluated
	ExitFatal   = 1 // Fatal errors (missing file, invalid config, etc.)
	ExitUsage   = 2 // Usage or flag error
	ExitPartial = 3 // Finished, but some profiles were unreachable (RateLimited, NetworkError, ParseError)
)

var (
	// Version info set at build time
	version = "v0.1.0-dev"
	commit  = "none"
	date    = "unknown"

	// Shared flags
	cfgPath        string
	flagGoal       int
	flagRPS        float64
	flagConcurrency int
	flagRefresh    bool
	flagNoCache    bool
	flagAnonymize  bool
	flagVerbose    bool
	flagNoColor    bool
	flagRunFormat    string
	flagExportFormat string
	flagDemoFormat   string
	flagOut          string
	flagRaw        bool
)

func isTerminal() bool {
	if os.Getenv("CI") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	return isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())
}

func shouldDisableColor() bool {
	if flagNoColor {
		return true
	}
	return os.Getenv("NO_COLOR") != "" || !isTerminal()
}

func loadAppConfig() (*config.Config, error) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, err
	}

	// Apply CLI overrides if explicitly set
	if flagGoal > 0 {
		cfg.Goal = flagGoal
	}
	if flagRPS > 0 {
		cfg.RateLimit.RPS = flagRPS
	}
	if flagConcurrency > 0 {
		cfg.RateLimit.Concurrency = flagConcurrency
	}

	return cfg, nil
}

func buildTracker(cfg *config.Config, refresh bool, noCache bool) (*tracker.Tracker, error) {
	limiter := ratelimit.New(cfg.RateLimit.RPS, cfg.RateLimit.Concurrency)
	fetcher := fetch.New(fetch.WithLimiter(limiter))

	opts := []tracker.Option{
		tracker.WithFetcher(fetcher),
		tracker.WithRefresh(refresh),
	}

	if noCache {
		opts = append(opts, tracker.WithNoCache())
	} else {
		ttl := time.Duration(cfg.Cache.TTLHours) * time.Hour
		c, err := cache.New(cfg.Cache.Dir, ttl)
		if err != nil {
			return nil, fmt.Errorf("initializing cache: %w", err)
		}
		opts = append(opts, tracker.WithCache(c))
	}

	return tracker.New(cfg, opts...)
}

func determineExitCode(results []model.ProfileResult) int {
	for _, r := range results {
		switch r.Status {
		case model.StatusRateLimited, model.StatusNetworkError, model.StatusParseError:
			return ExitPartial
		}
	}
	return ExitSuccess
}

func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "badgeboard",
		Short: "Track Google Skills badge progress for GDG Learn With Communities",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	rootCmd.PersistentFlags().StringVarP(&cfgPath, "config", "c", "", "Path to badgeboard.toml configuration file")
	rootCmd.PersistentFlags().IntVar(&flagGoal, "goal", 0, "Campaign badge goal (default 10)")
	rootCmd.PersistentFlags().Float64Var(&flagRPS, "rps", 0, "Max requests per second (default 1.0)")
	rootCmd.PersistentFlags().IntVar(&flagConcurrency, "concurrency", 0, "Max concurrent workers (default 2)")
	rootCmd.PersistentFlags().BoolVar(&flagRefresh, "refresh", false, "Bypass disk cache and re-fetch from upstream")
	rootCmd.PersistentFlags().BoolVar(&flagNoCache, "no-cache", false, "Disable on-disk cache (neither read nor write cache)")
	rootCmd.PersistentFlags().BoolVar(&flagAnonymize, "anonymize", false, "Pseudonymize participant names and mask PII for public output")
	rootCmd.PersistentFlags().BoolVarP(&flagVerbose, "verbose", "v", false, "Print verbose diagnostic logs")
	rootCmd.PersistentFlags().BoolVar(&flagNoColor, "no-color", false, "Disable colorized terminal output (respects NO_COLOR)")

	rootCmd.AddCommand(newRunCmd())
	rootCmd.AddCommand(newCheckCmd())
	rootCmd.AddCommand(newExportCmd())
	rootCmd.AddCommand(newDemoCmd())
	rootCmd.AddCommand(newCacheCmd())
	rootCmd.AddCommand(newVersionCmd())

	return rootCmd
}

func newRunCmd() *cobra.Command {
	runCmd := &cobra.Command{
		Use:   "run <roster.csv>",
		Short: "Track progress for all students in a CSV roster",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rosterPath := args[0]

			cfg, err := loadAppConfig()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			rst, err := roster.LoadFile(rosterPath)
			if err != nil {
				if errors.Is(err, roster.ErrEmptyRoster) {
					fmt.Fprintf(os.Stderr, "Error: roster file %q is completely empty.\n", rosterPath)
					os.Exit(ExitUsage)
				}
				return fmt.Errorf("loading roster %q: %w", rosterPath, err)
			}

			if len(rst.Students) == 0 {
				fmt.Fprintln(os.Stdout, "Roster contains zero student records.")
				return nil
			}

			trk, err := buildTracker(cfg, flagRefresh, flagNoCache)
			if err != nil {
				return fmt.Errorf("building tracker: %w", err)
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			noColor := shouldDisableColor()
			startTime := time.Now()

			var results []model.ProfileResult
			var cacheHits int

			if isTerminal() && flagRunFormat == "" {
				// Interactive Bubble Tea live progress
				liveModel := ui.NewLiveModel(ctx, stop, trk, rst, cfg.Goal, cfg.RateLimit.RPS, noColor)
				p := tea.NewProgram(liveModel, tea.WithOutput(os.Stderr))
				liveModel.StartTracking(p)

				if _, runErr := p.Run(); runErr != nil {
					return fmt.Errorf("UI program error: %w", runErr)
				}

				results, _ = liveModel.Results()
				cacheHits = liveModel.CacheHits()
			} else {
				// Non-TTY / scripted / CI output
				if len(rst.Students) > 100 {
					estDur := time.Duration(float64(len(rst.Students))/cfg.RateLimit.RPS) * time.Second
					fmt.Fprintf(os.Stderr, "Tracking %d profiles (estimated duration: ~%s at %.1f req/s)...\n",
						len(rst.Students), estDur.Round(time.Second), cfg.RateLimit.RPS)
				}

				var trackErr error
				results, trackErr = trk.Track(ctx, rst, func(current, total int, res model.ProfileResult) {
					if res.FromCache {
						cacheHits++
					}
					if flagRunFormat == "" && (current == 1 || current%10 == 0 || current == total) {
						elapsed := time.Since(startTime)
						netReqs := current - cacheHits
						netRate := 0.0
						if elapsed.Seconds() > 0 && netReqs > 0 {
							netRate = float64(netReqs) / elapsed.Seconds()
						}
						remaining := total - current
						etaDur := time.Duration(0)
						if remaining > 0 {
							effectiveNetRPS := netRate
							if effectiveNetRPS <= 0 {
								effectiveNetRPS = cfg.RateLimit.RPS
							}
							if effectiveNetRPS > 0 {
								etaDur = time.Duration(float64(remaining)/effectiveNetRPS) * time.Second
							}
						}
						pct := float64(current) / float64(total) * 100.0
						fmt.Fprintf(os.Stderr, "[%d/%d] %.1f%% | Cached: %d | Net Rate: %.1f req/s | ETA: %s\n",
							current, total, pct, cacheHits, netRate, etaDur.Round(time.Second))
					}
				})
				if trackErr != nil && len(results) == 0 {
					return fmt.Errorf("tracking error: %w", trackErr)
				}
			}

			// Format output if requested (JSON or CSV)
			if strings.EqualFold(flagRunFormat, "json") {
				return export.JSONWithBadRows(os.Stdout, results, rst.BadRows, cfg.Goal, flagAnonymize)
			} else if strings.EqualFold(flagRunFormat, "csv") {
				return export.CSV(os.Stdout, results, cfg.Goal, flagAnonymize)
			}

			// Render final Lip Gloss terminal table
			tableOpts := ui.TableOptions{
				Goal:      cfg.Goal,
				NoColor:   noColor,
				Width:     80,
				CacheHits: cacheHits,
				BadRows:   len(rst.BadRows),
				Duration:  time.Since(startTime),
			}
			tableOutput := ui.RenderFinalTable(results, tableOpts)
			fmt.Print(tableOutput)

			if !flagNoCache && trk.WritesToCache() > 0 {
				fmt.Fprintf(os.Stderr, "Cached profile data is stored in %s. Use --no-cache or badgeboard cache clear for sensitive runs.\n", trk.CacheDir())
			}

			exitCode := determineExitCode(results)
			if exitCode != ExitSuccess {
				os.Exit(exitCode)
			}

			return nil
		},
	}

	runCmd.Flags().StringVar(&flagRunFormat, "format", "", "Output format for stdout (json or csv)")
	return runCmd
}

func newCheckCmd() *cobra.Command {
	checkCmd := &cobra.Command{
		Use:   "check <url>",
		Short: "Fetch and evaluate a single Google Skills profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rawURL := args[0]
			canonicalURL, uuid, err := roster.NormalizeURL(rawURL)
			if err != nil {
				return fmt.Errorf("invalid profile URL %q: %w", rawURL, err)
			}

			cfg, err := loadAppConfig()
			if err != nil {
				return err
			}

			f := fetch.New()
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			html, err := f.Fetch(ctx, canonicalURL)
			if err != nil {
				if errors.Is(err, fetch.ErrNotFoundOrPrivate) {
					fmt.Printf("Profile %s is private or not found (404/sign-in redirect).\n", canonicalURL)
					return nil
				}
				return fmt.Errorf("fetching profile: %w", err)
			}

			data, err := parse.Parse(html)
			if err != nil {
				return fmt.Errorf("parsing profile: %w", err)
			}

			status := model.EvaluateStatus(len(data.Badges), cfg.Goal, len(data.Badges), data.IsPrivate)
			res := model.ProfileResult{
				Student: model.Student{
					ProfileURL: canonicalURL,
					UUID:       uuid,
					Name:       data.DisplayName,
				},
				DisplayName:   data.DisplayName,
				Status:        status,
				CountedBadges: len(data.Badges),
				TotalBadges:   len(data.Badges),
				Badges:        data.Badges,
				Points:        data.Points,
				League:        data.League,
				FetchedAt:     time.Now().UTC(),
			}

			if flagRaw {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(res)
			}

			noColor := shouldDisableColor()
			fmt.Printf("Display Name: %s\n", res.DisplayName)
			fmt.Printf("Profile URL:  %s\n", res.Student.ProfileURL)
			fmt.Printf("Status:       %s\n", ui.StatusLabel(res.Status, noColor))
			fmt.Printf("Badges:       %d / %d goal\n", res.CountedBadges, cfg.Goal)
			if res.League != "" {
				fmt.Printf("League:       %s\n", res.League)
			}
			if res.Points > 0 {
				fmt.Printf("Points:       %d\n", res.Points)
			}

			fmt.Println("\nEarned Badges:")
			if len(res.Badges) == 0 {
				fmt.Println("  (No skill badges found on profile)")
			} else {
				for i, b := range res.Badges {
					earnedStr := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(b.RawEarned), "Earned"))
					if earnedStr == "" && !b.EarnedAt.IsZero() {
						earnedStr = b.EarnedAt.Format("Jan 2, 2006")
					}
					fmt.Printf("  %2d. %-45s  (Earned: %s)\n", i+1, b.Title, earnedStr)
				}
			}

			return nil
		},
	}

	checkCmd.Flags().BoolVar(&flagRaw, "raw", false, "Output full parsed profile data as JSON")
	return checkCmd
}

func newExportCmd() *cobra.Command {
	exportCmd := &cobra.Command{
		Use:   "export <roster.csv>",
		Short: "Export evaluated profile progress to a JSON or CSV file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rosterPath := args[0]

			cfg, err := loadAppConfig()
			if err != nil {
				return err
			}

			rst, err := roster.LoadFile(rosterPath)
			if err != nil {
				return err
			}

			trk, err := buildTracker(cfg, flagRefresh, flagNoCache)
			if err != nil {
				return err
			}

			results, err := trk.Track(cmd.Context(), rst, nil)
			if err != nil && len(results) == 0 {
				return err
			}

			outWriter := os.Stdout
			if flagOut != "" && flagOut != "-" {
				if err := os.MkdirAll(filepath.Dir(flagOut), 0755); err != nil && filepath.Dir(flagOut) != "." {
					return fmt.Errorf("creating output directory: %w", err)
				}
				f, err := os.Create(flagOut)
				if err != nil {
					return fmt.Errorf("creating output file %q: %w", flagOut, err)
				}
				defer f.Close()
				outWriter = f
			}

			var exportErr error
			if strings.EqualFold(flagExportFormat, "csv") {
				exportErr = export.CSV(outWriter, results, cfg.Goal, flagAnonymize)
			} else {
				exportErr = export.JSONWithBadRows(outWriter, results, rst.BadRows, cfg.Goal, flagAnonymize)
			}
			if exportErr != nil {
				return exportErr
			}

			if !flagNoCache && trk.WritesToCache() > 0 {
				fmt.Fprintf(os.Stderr, "Cached profile data is stored in %s. Use --no-cache or badgeboard cache clear for sensitive runs.\n", trk.CacheDir())
			}
			return nil
		},
	}

	exportCmd.Flags().StringVar(&flagExportFormat, "format", "json", "Output format (json or csv)")
	exportCmd.Flags().StringVarP(&flagOut, "out", "o", "", "Destination file path (defaults to stdout)")
	return exportCmd
}

func newCacheCmd() *cobra.Command {
	cacheCmd := &cobra.Command{
		Use:   "cache",
		Short: "Manage the local profile cache",
	}

	clearCmd := &cobra.Command{
		Use:   "clear",
		Short: "Clear all cached profile entries from disk",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadAppConfig()
			if err != nil {
				return err
			}
			c, err := cache.New(cfg.Cache.Dir, time.Duration(cfg.Cache.TTLHours)*time.Hour)
			if err != nil {
				return fmt.Errorf("opening cache: %w", err)
			}
			if err := c.Clear(); err != nil {
				return fmt.Errorf("clearing cache: %w", err)
			}
			fmt.Printf("Cleared cache directory: %s\n", c.Dir())
			return nil
		},
	}

	cacheCmd.AddCommand(clearCmd)
	return cacheCmd
}

func newDemoCmd() *cobra.Command {
	demoCmd := &cobra.Command{
		Use:   "demo",
		Short: "Generate deterministic synthetic data for dashboard development",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _ := loadAppConfig()
			goal := cfg.Goal
			if goal <= 0 {
				goal = 10
			}

			// Generate deterministic synthetic results using fixed seed (seed: 42)
			results := generateDeterministicDemoData(goal)

			format := strings.ToLower(strings.TrimSpace(flagDemoFormat))
			if format == "" {
				format = "text"
			}

			switch format {
			case "json":
				outWriter := os.Stdout
				if flagOut != "" && flagOut != "-" {
					if err := os.MkdirAll(filepath.Dir(flagOut), 0755); err != nil && filepath.Dir(flagOut) != "." {
						return fmt.Errorf("creating output directory: %w", err)
					}
					f, err := os.Create(flagOut)
					if err != nil {
						return fmt.Errorf("creating demo output file %q: %w", flagOut, err)
					}
					defer f.Close()
					outWriter = f
				}
				return export.JSONWithBadRows(outWriter, results, nil, goal, flagAnonymize)

			case "csv":
				outWriter := os.Stdout
				if flagOut != "" && flagOut != "-" {
					if err := os.MkdirAll(filepath.Dir(flagOut), 0755); err != nil && filepath.Dir(flagOut) != "." {
						return fmt.Errorf("creating output directory: %w", err)
					}
					f, err := os.Create(flagOut)
					if err != nil {
						return fmt.Errorf("creating demo output file %q: %w", flagOut, err)
					}
					defer f.Close()
					outWriter = f
				}
				return export.CSV(outWriter, results, goal, flagAnonymize)

			case "text":
				outPath := flagOut
				if outPath == "" {
					outPath = "data/results.json"
				}
				if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil && filepath.Dir(outPath) != "." {
					return fmt.Errorf("creating output directory: %w", err)
				}
				f, err := os.Create(outPath)
				if err != nil {
					return fmt.Errorf("creating demo output file %q: %w", outPath, err)
				}
				defer f.Close()

				if err := export.JSONWithBadRows(f, results, nil, goal, flagAnonymize); err != nil {
					return fmt.Errorf("writing demo data: %w", err)
				}

				noColor := shouldDisableColor()
				fmt.Printf("Generated %d synthetic demo records -> %s\n\n", len(results), outPath)
				tableOpts := ui.TableOptions{
					Goal:      goal,
					NoColor:   noColor,
					Width:     80,
					CacheHits: 8,
					BadRows:   0,
					Duration:  1800 * time.Millisecond,
				}
				fmt.Print(ui.RenderFinalTable(results, tableOpts))
				return nil

			default:
				return fmt.Errorf("unsupported demo format %q (choose text, json, or csv)", flagDemoFormat)
			}
		},
	}

	demoCmd.Flags().StringVar(&flagDemoFormat, "format", "text", "Output format (text, json, or csv)")
	demoCmd.Flags().StringVarP(&flagOut, "out", "o", "", "Destination file path")
	return demoCmd
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print badgeboard version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintf(cmd.OutOrStdout(), "badgeboard %s (commit: %s, built: %s)\n", version, commit, date)
		},
	}
}

func generateDeterministicDemoData(goal int) []model.ProfileResult {
	rng := rand.New(rand.NewSource(42))

	badgeTitles := []string{
		"Deploy Kubernetes Applications on Google Cloud",
		"Build a Secure Google Cloud Network",
		"Set Up and Configure a Cloud Environment in Google Cloud",
		"Perform Foundational Infrastructure Tasks in Google Cloud",
		"Create and Manage Cloud Spanner Instances",
		"Engineer Data for Predictive Modeling with BigQuery ML",
		"Implement DevOps in Google Cloud",
		"Build Infrastructure with Terraform on Google Cloud",
		"Monitor and Log with Google Cloud Observability",
		"Implement Cloud Security Fundamentals on Google Cloud",
	}

	// 15 clearly fictional names with realistic spread:
	// 4 complete, 5 in progress, 2 no badges, 2 private, 1 network error, 1 rate limited
	students := []struct {
		name   string
		uuid   string
		status model.Status
		count  int
	}{
		{"Orion Sterling", "11111111-aaaa-bbbb-cccc-000000000001", model.StatusComplete, 10},
		{"Lyra Vance", "11111111-aaaa-bbbb-cccc-000000000002", model.StatusComplete, 10},
		{"Corin Calder", "11111111-aaaa-bbbb-cccc-000000000003", model.StatusComplete, 10},
		{"Zephyr Drake", "11111111-aaaa-bbbb-cccc-000000000004", model.StatusComplete, 10},
		{"Astra Thorne", "11111111-aaaa-bbbb-cccc-000000000005", model.StatusInProgress, 8},
		{"Talon Mercer", "11111111-aaaa-bbbb-cccc-000000000006", model.StatusInProgress, 6},
		{"Vesper Rowe", "11111111-aaaa-bbbb-cccc-000000000007", model.StatusInProgress, 5},
		{"Cassian Vane", "11111111-aaaa-bbbb-cccc-000000000008", model.StatusInProgress, 3},
		{"Sonya Vale", "11111111-aaaa-bbbb-cccc-000000000009", model.StatusInProgress, 1},
		{"Jaxen Cross", "11111111-aaaa-bbbb-cccc-000000000010", model.StatusNoBadges, 0},
		{"Rylan Solis", "11111111-aaaa-bbbb-cccc-000000000011", model.StatusNoBadges, 0},
		{"Kaelen Frost", "11111111-aaaa-bbbb-cccc-000000000012", model.StatusPrivateOrNotFound, 0},
		{"Thalia Voss", "11111111-aaaa-bbbb-cccc-000000000013", model.StatusPrivateOrNotFound, 0},
		{"Brecken Hale", "11111111-aaaa-bbbb-cccc-000000000014", model.StatusNetworkError, 0},
		{"Silas Novak", "11111111-aaaa-bbbb-cccc-000000000015", model.StatusRateLimited, 0},
	}

	results := make([]model.ProfileResult, len(students))
	now := time.Now().UTC()

	for i, s := range students {
		url := fmt.Sprintf("https://www.skills.google/public_profiles/%s", s.uuid)
		res := model.ProfileResult{
			Student: model.Student{
				Name:       s.name,
				UUID:       s.uuid,
				ProfileURL: url,
				RowNumber:  i + 2,
			},
			DisplayName:   s.name,
			Status:        s.status,
			CountedBadges: s.count,
			TotalBadges:   s.count,
			Points:        s.count * 30,
			League:        "Gold League",
			FetchedAt:     now,
		}

		if s.count > 0 {
			badges := make([]model.BadgeRecord, s.count)
			for b := 0; b < s.count; b++ {
				daysAgo := 25 - (b*2 + (i % 3))
				if daysAgo < 1 {
					daysAgo = 1
				}
				earnedDate := now.Add(-time.Duration(daysAgo)*24*time.Hour - time.Duration((i*5+b*7+rng.Intn(3))%24)*time.Hour)
				badges[b] = model.BadgeRecord{
					ID:        fmt.Sprintf("badge-%d", b+1),
					Title:     badgeTitles[b%len(badgeTitles)],
					EarnedAt:  earnedDate,
					RawEarned: earnedDate.Format("Jan 2, 2006 EST"),
					URL:       fmt.Sprintf("%s/badges/%d", url, 100+b),
				}
			}
			res.Badges = badges
			last := badges[len(badges)-1].EarnedAt
			res.LastBadgeEarnedAt = &last
		}

		if s.status == model.StatusPrivateOrNotFound {
			res.Error = "profile is set to private or return 404"
		} else if s.status == model.StatusNetworkError {
			res.Error = "network timeout dialing skills.google"
		} else if s.status == model.StatusRateLimited {
			res.Error = "HTTP 429: rate limit exceeded"
		}

		results[i] = res
	}

	return results
}
