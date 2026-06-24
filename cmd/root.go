package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/confluentinc/schema-deletion-tool/pkg"
	"github.com/spf13/cobra"
)

func runScan(cmd *cobra.Command, _ []string) error {
	platformFlag, _ := cmd.Flags().GetString("platform")
	strategy, _ := cmd.Flags().GetString("strategy")
	subject, _ := cmd.Flags().GetString("subject")
	all, _ := cmd.Flags().GetBool("all-subjects")
	configFile, _ := cmd.Flags().GetString("config-file")
	topicsFlag, _ := cmd.Flags().GetString("topics")
	scanAllTopics, _ := cmd.Flags().GetBool("all-topics")
	contextFlag, _ := cmd.Flags().GetString("context")
	outputFile, _ := cmd.Flags().GetString("output")
	force, _ := cmd.Flags().GetBool("force")
	workers, _ := cmd.Flags().GetInt("workers")
	scanTimeout, _ := cmd.Flags().GetDuration("scan-timeout")
	srURL, _ := cmd.Flags().GetString("sr-url")
	srAPIKey, _ := cmd.Flags().GetString("sr-api-key")
	srAPISecret, _ := cmd.Flags().GetString("sr-api-secret")

	if workers < 1 {
		workers = 1
	}
	if workers > 100 {
		workers = 100
	}
	if scanTimeout <= 0 {
		return errors.New("--scan-timeout must be positive")
	}

	// Validate scan-specific flags
	if !cmd.Flags().Changed("all-subjects") && !cmd.Flags().Changed("subject") {
		return errors.New("at least one of --subject or --all-subjects must be specified")
	}
	if cmd.Flags().Changed("subject") && strategy == "topic-name" && !pkg.VerifySubject(subject) {
		return errors.New("subject does not match TopicNameStrategy (must end with -key or -value)")
	}
	if strategy == "record-name" && topicsFlag == "" && !scanAllTopics {
		return errors.New("--topics or --all-topics is required with --strategy=record-name")
	}
	if platformFlag == "cp" && configFile == "" {
		return errors.New("--config-file is required when --platform=cp")
	}

	cmd.SilenceUsage = true

	// Create platform
	platform, err := createPlatform(platformFlag, configFile, srURL, srAPIKey, srAPISecret)
	if err != nil {
		return err
	}

	// Get subjects
	var subjects []string
	if all {
		subjects, err = platform.ListSubjects(strategy)
		if err != nil {
			return err
		}
		if cmd.Flags().Changed("context") {
			var filtered []string
			for _, s := range subjects {
				if pkg.GetContextFromSubject(s) == contextFlag {
					filtered = append(filtered, s)
				}
			}
			subjects = filtered
		}
		if len(subjects) == 0 {
			fmt.Println("No eligible subjects found.")
			return nil
		}
		fmt.Printf("Found %d eligible subject(s).\n", len(subjects))
	} else {
		subjects = []string{subject}
	}

	// Resolve explicit topics
	var explicitTopics []string
	if topicsFlag != "" {
		explicitTopics = strings.Split(topicsFlag, ",")
	}

	// Set up context (cluster credentials)
	ctx, err := setupContext(platform, platformFlag, configFile, force)
	if err != nil {
		return err
	}

	// Resolve topics to scan
	topicsWithCluster, err := pkg.ResolveTopics(subjects, strategy, explicitTopics, scanAllTopics, platform, ctx.Clusters)
	if err != nil {
		return err
	}

	if len(topicsWithCluster) == 0 {
		fmt.Println("No matching topics found. All schemas will be treated as unused.")
	}

	pkg.PrintTable(pkg.TopicInfoFields, topicsWithCluster, false)

	// Get all schemas
	schemas, err := platform.ListSchemas(subjects)
	if err != nil {
		return err
	}
	if len(schemas) == 0 {
		fmt.Println("No schemas found.")
		return nil
	}

	// Scan topics for active schema IDs
	activeSchemas, failedTopics, err := scanTopicsForActiveSchemas(topicsWithCluster, platform, ctx, workers, scanTimeout)
	if err != nil {
		return err
	}

	scannedTopics := make([]string, len(topicsWithCluster))
	for i, t := range topicsWithCluster {
		scannedTopics[i] = t.Topic
	}

	// Subjects tied to a topic we couldn't scan have unverifiable usage; block
	// them so a partial scan can never delete a schema that is still in use.
	unverifiedSubjects := pkg.SubjectsForFailedTopics(failedTopics, subjects, scannedTopics, strategy)
	if len(failedTopics) > 0 {
		fmt.Printf("\n%s%d topic(s) could not be scanned; %d subject(s) tied to them will be marked unverified and excluded from deletion:%s\n",
			pkg.YELLOW, len(failedTopics), len(unverifiedSubjects), pkg.RESET)
		for _, t := range failedTopics {
			fmt.Printf("  - %s\n", t)
		}
	}

	// Run safety analysis
	fmt.Println("\nAnalyzing deletion candidates...")
	candidates, err := pkg.AnalyzeCandidates(schemas, activeSchemas, platform, strategy)
	if err != nil {
		return err
	}

	candidates = pkg.CheckCompatibility(candidates, platform)
	candidates = pkg.MarkUnverified(candidates, unverifiedSubjects)

	if len(candidates) == 0 {
		fmt.Println("No unused schemas found.")
		return nil
	}

	printCandidateSummary(candidates)

	// Write manifest if output specified
	if outputFile != "" {
		return pkg.WriteManifest(outputFile, candidates, pkg.ManifestOptions{
			Platform:         platformFlag,
			Strategy:         strategy,
			ScannedTopics:    scannedTopics,
			ScannedClusters:  ctx.Clusters,
			ActiveSchemaIDs:  activeSchemas,
			UnverifiedTopics: failedTopics,
		})
	}

	fmt.Println("\nScan complete. Use --output <file> to save manifest for deletion.")
	return nil
}

func runDelete(cmd *cobra.Command, _ []string) error {
	platformFlag, _ := cmd.Flags().GetString("platform")
	configFile, _ := cmd.Flags().GetString("config-file")
	fromFile, _ := cmd.Flags().GetString("from-file")
	mode, _ := cmd.Flags().GetString("mode")
	force, _ := cmd.Flags().GetBool("force")

	if platformFlag == "cp" && configFile == "" {
		return errors.New("--config-file is required when --platform=cp")
	}

	var softDelete, hardDelete bool
	switch mode {
	case "soft":
		softDelete = true
	case "hard":
		hardDelete = true
	case "full":
		softDelete = true
		hardDelete = true
	default:
		return fmt.Errorf("unknown mode: %s (use 'soft', 'hard', or 'full')", mode)
	}

	cmd.SilenceUsage = true

	manifest, err := pkg.ReadManifest(fromFile)
	if err != nil {
		return err
	}

	platform, err := createPlatform(platformFlag, configFile, "", "", "")
	if err != nil {
		return err
	}

	fmt.Printf("Loaded manifest with %d candidate(s) (generated %s)\n", len(manifest.Candidates), manifest.GeneratedAt)
	return pkg.ExecuteDeletion(manifest.Candidates, platform, softDelete, hardDelete, force)
}

func createPlatform(platformFlag, configFile, srURL, srAPIKey, srAPISecret string) (pkg.Platform, error) {
	switch platformFlag {
	case "cloud":
		cp := pkg.NewCloudPlatform()
		if srURL != "" && srAPIKey != "" {
			cp.SetSRCredentials(srURL, srAPIKey, srAPISecret)
		} else {
			fmt.Println("Note: --sr-url and --sr-api-key not provided. Schema reference checking will be skipped for Cloud.")
			fmt.Println("Provide SR credentials to enable reference safety checks.")
		}
		return cp, nil
	case "cp":
		if configFile == "" {
			return nil, errors.New("--config-file is required when --platform=cp")
		}
		config, err := pkg.LoadCPConfig(configFile)
		if err != nil {
			return nil, err
		}
		return pkg.NewCPPlatform(*config)
	default:
		return nil, fmt.Errorf("unknown platform: %s (use 'cloud' or 'cp')", platformFlag)
	}
}

func setupContext(platform pkg.Platform, platformFlag, configFile string, force bool) (*pkg.Context, error) {
	// For CP, cluster credentials are in the platform config, not a separate file
	contextConfigFile := configFile
	if platformFlag == "cp" {
		contextConfigFile = ""
	}

	ctx, err := pkg.NewContext(contextConfigFile)
	if err != nil {
		return nil, err
	}

	if platformFlag == "cp" {
		// For CP, clusters are defined in the config file. No interactive prompting needed.
		clusters, err := platform.ListClusters()
		if err != nil {
			return nil, err
		}
		var clusterIDs []string
		for _, c := range clusters {
			clusterIDs = append(clusterIDs, c.ID)
		}
		ctx.Clusters = clusterIDs
		return ctx, nil
	}

	// Cloud: list clusters and prompt for credentials
	fmt.Println("Listing all clusters under the environment...")
	clusters, err := platform.ListClusters()
	if err != nil {
		return nil, err
	}

	var clusterIDs []string
	if force {
		// Non-interactive: scan all clusters, no prompting
		for _, c := range clusters {
			clusterIDs = append(clusterIDs, c.ID)
		}
	} else {
		// Interactive: display clusters and ask which to skip
		clusterInfos := make([]pkg.ClusterInfo, len(clusters))
		copy(clusterInfos, clusters)

		fmt.Print("Please select the clusters you want to skip, with cluster IDs separated by comma: ")
		resp, err := pkg.ReadLine()
		if err != nil {
			return nil, err
		}
		skipped := make(map[string]struct{})
		if resp != "" {
			for _, id := range strings.Split(resp, ",") {
				skipped[strings.TrimSpace(id)] = struct{}{}
			}
		}
		for _, c := range clusters {
			if _, ok := skipped[c.ID]; !ok {
				clusterIDs = append(clusterIDs, c.ID)
			}
		}
	}

	return ctx, ctx.SetClusters(clusterIDs, force)
}

// scanResult is the outcome of scanning a single topic.
type scanResult struct {
	schemas map[int32]int
	err     error
	topic   string
	cluster string
}

// scanTopicsForActiveSchemas scans every topic and returns the merged active
// schema IDs plus the names of topics that could not be scanned. Per-topic
// failures do not abort the run; callers must treat schemas tied to the returned
// failedTopics as unverified. A non-nil error is only returned for setup
// failures that prevent scanning entirely (e.g. cluster endpoint resolution).
func scanTopicsForActiveSchemas(topics []pkg.TopicWithClusterInfo, platform pkg.Platform, ctx *pkg.Context, workers int, scanTimeout time.Duration) (map[int32]int, []string, error) {
	if len(topics) == 0 {
		return make(map[int32]int), nil, nil
	}

	// Resolve endpoints per cluster (cache to avoid repeated calls)
	endpoints := make(map[string]string)
	for _, topic := range topics {
		if _, ok := endpoints[topic.ClusterID]; !ok {
			endpoint, err := platform.DescribeCluster(topic.ClusterID)
			if err != nil {
				return nil, nil, err
			}
			endpoints[topic.ClusterID] = endpoint
		}
	}

	// Scan topics concurrently
	results := make(chan scanResult, len(topics))
	sem := make(chan struct{}, workers)

	for _, topic := range topics {
		sem <- struct{}{} // acquire semaphore
		go func(t pkg.TopicWithClusterInfo) {
			defer func() { <-sem }() // release semaphore

			fmt.Printf("Scanning topic %s%s%s from cluster %s...\n", pkg.GREEN, t.Topic, pkg.RESET, t.ClusterID)

			creds := ctx.Credentials[t.ClusterID]
			ccfg, err := platform.CreateConsumerConfig(t.ClusterID, creds)
			if err != nil {
				results <- scanResult{err: err, topic: t.Topic, cluster: t.ClusterID}
				return
			}
			if err = ccfg.SetKey("bootstrap.servers", endpoints[t.ClusterID]); err != nil {
				results <- scanResult{err: err, topic: t.Topic, cluster: t.ClusterID}
				return
			}

			consumer, err := pkg.CreateConsumerFromConfig(ccfg)
			if err != nil {
				results <- scanResult{err: err, topic: t.Topic, cluster: t.ClusterID}
				return
			}
			defer consumer.Close()

			topicSchemas, err := pkg.ScanActiveSchemas(consumer, t.Topic, scanTimeout)
			results <- scanResult{schemas: topicSchemas, err: err, topic: t.Topic, cluster: t.ClusterID}
		}(topic)
	}

	// Collect every result before deciding. A single unscannable topic must not
	// discard the work done for the others, and the user should see all failures
	// at once instead of fixing them one re-run at a time.
	collected := make([]scanResult, 0, len(topics))
	for i := 0; i < len(topics); i++ {
		r := <-results
		if r.err != nil {
			fmt.Printf("%swarning: failed to scan topic %s (cluster %s): %v%s\n", pkg.YELLOW, r.topic, r.cluster, r.err, pkg.RESET)
		}
		collected = append(collected, r)
	}

	activeSchemas, failedTopics := mergeActiveSchemas(collected)
	return activeSchemas, failedTopics, nil
}

// mergeActiveSchemas OR-merges per-topic schema results and returns the names of
// topics that failed to scan.
func mergeActiveSchemas(results []scanResult) (map[int32]int, []string) {
	activeSchemas := make(map[int32]int)
	var failedTopics []string
	for _, r := range results {
		if r.err != nil {
			failedTopics = append(failedTopics, r.topic)
			continue
		}
		for k, v := range r.schemas {
			activeSchemas[k] = activeSchemas[k] | v
		}
	}
	return activeSchemas, failedTopics
}

func printCandidateSummary(candidates []pkg.DeletionCandidate) {
	safe := pkg.GetSafeCandidates(candidates)
	blocked := pkg.GetBlockedCandidates(candidates)
	warned := pkg.GetWarnedCandidates(candidates)

	fmt.Printf("\nAnalysis complete: %d candidate(s)\n", len(candidates))
	fmt.Printf("  %s%d safe to delete%s\n", pkg.GREEN, len(safe), pkg.RESET)
	if len(warned) > 0 {
		fmt.Printf("  %d with warnings (deletable with confirmation)\n", len(warned))
	}
	if len(blocked) > 0 {
		fmt.Printf("  %s%d blocked from deletion%s\n", pkg.RED, len(blocked), pkg.RESET)
	}
}

func Execute() {
	var rootCmd = &cobra.Command{
		Use:   "confluent schema-registry cleanup",
		Short: "Schema deletion tool - discover and delete unused schemas",
		Long: `Schema deletion tool - discover unused schemas from Kafka topics
and delete them from Schema Registry.

Supports both Confluent Cloud and Confluent Platform.

Workflow:
  1. scan   - Discover unused schemas and save a manifest
  2. delete - Delete schemas from a manifest`,
	}

	// Common flags (persistent = inherited by subcommands)
	rootCmd.PersistentFlags().String("platform", "cloud", "Platform type: 'cloud' or 'cp' (Confluent Platform).")
	rootCmd.PersistentFlags().String("config-file", "", "Path to config file (cluster credentials for Cloud, full config for CP).")
	rootCmd.PersistentFlags().Bool("force", false, "Skip interactive confirmation prompts.")

	// Scan subcommand
	scanCmd := &cobra.Command{
		Use:   "scan",
		Short: "Scan topics and analyze schemas for deletion candidates",
		Long: `Scan Kafka topics to find active schema IDs, then analyze all registered
schemas to identify unused versions that are candidates for deletion.

This is a read-only operation. Use --output to save results as a manifest
file that can be passed to the delete command.`,
		RunE: runScan,
	}
	scanCmd.Flags().StringP("subject", "V", "", "Subject to analyze.")
	scanCmd.Flags().Bool("all-subjects", false, "Analyze all eligible subjects.")
	scanCmd.Flags().String("strategy", "topic-name", "Subject naming strategy: 'topic-name', 'record-name', 'topic-record-name'.")
	scanCmd.Flags().String("topics", "", "Comma-separated list of topics to scan.")
	scanCmd.Flags().Bool("all-topics", false, "Scan all topics across all clusters.")
	scanCmd.Flags().String("context", "", "Schema context to scope operations to (e.g., 'staging').")
	scanCmd.Flags().Int("workers", 25, "Number of concurrent topic scanners.")
	scanCmd.Flags().Duration("scan-timeout", 5*time.Minute, "Per-topic scan deadline; topics exceeding it are marked unverified and excluded from deletion.")
	scanCmd.Flags().String("output", "", "Path to write manifest file.")
	scanCmd.Flags().String("sr-url", "", "Schema Registry URL (enables reference checking for Cloud).")
	scanCmd.Flags().String("sr-api-key", "", "Schema Registry API key (for reference checking).")
	scanCmd.Flags().String("sr-api-secret", "", "Schema Registry API secret (for reference checking).")
	scanCmd.MarkFlagsMutuallyExclusive("subject", "all-subjects")
	scanCmd.MarkFlagsMutuallyExclusive("topics", "all-topics")

	// Delete subcommand
	deleteCmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete schemas from a scan manifest",
		Long: `Delete schema versions listed in a manifest file produced by the scan command.

Deletion modes:
  soft - Soft-delete only (default, reversible)
  hard - Hard-delete only (for already soft-deleted schemas)
  full - Soft-delete followed by hard-delete (permanent)`,
		RunE: runDelete,
	}
	deleteCmd.Flags().String("from-file", "", "Path to manifest file from scan (required).")
	deleteCmd.Flags().String("mode", "soft", "Deletion mode: 'soft', 'hard', or 'full'.")
	deleteCmd.MarkFlagRequired("from-file")

	rootCmd.AddCommand(scanCmd, deleteCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
