package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/confluentinc/schema-deletion-tool/pkg"
	"github.com/spf13/cobra"
)

func run(cmd *cobra.Command, _ []string) error {
	// Parse flags
	platformFlag, _ := cmd.Flags().GetString("platform")
	strategy, _ := cmd.Flags().GetString("strategy")
	subject, _ := cmd.Flags().GetString("subject")
	all, _ := cmd.Flags().GetBool("all")
	configFile, _ := cmd.Flags().GetString("config-file")
	cpConfigFile, _ := cmd.Flags().GetString("cp-config-file")
	topicsFlag, _ := cmd.Flags().GetString("topics")
	scanAllTopics, _ := cmd.Flags().GetBool("scan-all-topics")
	contextFlag, _ := cmd.Flags().GetString("context")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	outputFile, _ := cmd.Flags().GetString("output")
	fromFile, _ := cmd.Flags().GetString("from-file")
	softDelete, _ := cmd.Flags().GetBool("soft-delete")
	hardDelete, _ := cmd.Flags().GetBool("hard-delete")
	force, _ := cmd.Flags().GetBool("force")
	workers, _ := cmd.Flags().GetInt("workers")
	if workers < 1 {
		workers = 1
	}
	if workers > 100 {
		workers = 100
	}

	// --output implies --dry-run
	if outputFile != "" {
		dryRun = true
	}

	// Validate flag combinations
	if err := validateFlags(cmd, platformFlag, strategy, fromFile, dryRun, softDelete, hardDelete, force, cpConfigFile, topicsFlag, scanAllTopics); err != nil {
		return err
	}

	cmd.SilenceUsage = true

	// ---- FROM-FILE MODE: skip scanning, go straight to deletion ----
	if fromFile != "" {
		return runFromFile(fromFile, platformFlag, cpConfigFile, configFile, softDelete, hardDelete, force)
	}

	// ---- DISCOVERY MODE: scan topics and find candidates ----

	// Create platform
	platform, err := createPlatform(platformFlag, cpConfigFile)
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
		// Filter by context if --context flag was explicitly set
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

	// List clusters and set up credentials
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
	activeSchemas, err := scanTopicsForActiveSchemas(topicsWithCluster, platform, ctx, workers)
	if err != nil {
		return err
	}

	// Run safety analysis
	fmt.Println("\nAnalyzing deletion candidates...")
	candidates, err := pkg.AnalyzeCandidates(schemas, activeSchemas, platform, strategy)
	if err != nil {
		return err
	}

	candidates = pkg.CheckCompatibility(candidates, platform)

	if len(candidates) == 0 {
		fmt.Println("No unused schemas found.")
		return nil
	}

	// Print summary
	printCandidateSummary(candidates)

	// Dry run: output manifest and exit
	if dryRun {
		if outputFile != "" {
			scannedTopics := make([]string, len(topicsWithCluster))
			for i, t := range topicsWithCluster {
				scannedTopics[i] = t.Topic
			}
			return pkg.WriteManifest(outputFile, candidates, pkg.ManifestOptions{
				Platform:        platformFlag,
				Strategy:        strategy,
				ScannedTopics:   scannedTopics,
				ScannedClusters: ctx.Clusters,
				ActiveSchemaIDs: activeSchemas,
			})
		}
		fmt.Println("\nDry run complete. Use --output <file> to save manifest for later deletion.")
		return nil
	}

	// Interactive deletion (original behavior, enhanced)
	if !softDelete && !hardDelete {
		softDelete = true
		hardDelete = true
	}

	return pkg.ExecuteDeletion(candidates, platform, softDelete, hardDelete, force)
}

func runFromFile(fromFile, platformFlag, cpConfigFile, configFile string, softDelete, hardDelete, force bool) error {
	manifest, err := pkg.ReadManifest(fromFile)
	if err != nil {
		return err
	}

	platform, err := createPlatform(platformFlag, cpConfigFile)
	if err != nil {
		return err
	}

	fmt.Printf("Loaded manifest with %d candidate(s) (generated %s)\n", len(manifest.Candidates), manifest.GeneratedAt)
	return pkg.ExecuteDeletion(manifest.Candidates, platform, softDelete, hardDelete, force)
}

func createPlatform(platformFlag, cpConfigFile string) (pkg.Platform, error) {
	switch platformFlag {
	case "cloud":
		return pkg.NewCloudPlatform(), nil
	case "cp":
		if cpConfigFile == "" {
			return nil, errors.New("--cp-config-file is required when --platform=cp")
		}
		config, err := pkg.LoadCPConfig(cpConfigFile)
		if err != nil {
			return nil, err
		}
		return pkg.NewCPPlatform(*config)
	default:
		return nil, fmt.Errorf("unknown platform: %s (use 'cloud' or 'cp')", platformFlag)
	}
}

func setupContext(platform pkg.Platform, platformFlag, configFile string, force bool) (*pkg.Context, error) {
	ctx, err := pkg.NewContext(configFile)
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

func scanTopicsForActiveSchemas(topics []pkg.TopicWithClusterInfo, platform pkg.Platform, ctx *pkg.Context, workers int) (map[int32]int, error) {
	if len(topics) == 0 {
		return make(map[int32]int), nil
	}

	// Resolve endpoints per cluster (cache to avoid repeated calls)
	endpoints := make(map[string]string)
	for _, topic := range topics {
		if _, ok := endpoints[topic.ClusterID]; !ok {
			endpoint, err := platform.DescribeCluster(topic.ClusterID)
			if err != nil {
				return nil, err
			}
			endpoints[topic.ClusterID] = endpoint
		}
	}

	// Scan topics concurrently
	type scanResult struct {
		schemas map[int32]int
		err     error
		topic   string
	}

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
				results <- scanResult{err: err, topic: t.Topic}
				return
			}
			if err = ccfg.SetKey("bootstrap.servers", endpoints[t.ClusterID]); err != nil {
				results <- scanResult{err: err, topic: t.Topic}
				return
			}

			consumer, err := pkg.CreateConsumerFromConfig(ccfg)
			if err != nil {
				results <- scanResult{err: err, topic: t.Topic}
				return
			}
			defer consumer.Close()

			topicSchemas, err := pkg.ScanActiveSchemas(consumer, t.Topic)
			results <- scanResult{schemas: topicSchemas, err: err, topic: t.Topic}
		}(topic)
	}

	// Collect results
	activeSchemas := make(map[int32]int)
	var firstErr error
	for i := 0; i < len(topics); i++ {
		r := <-results
		if r.err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("error scanning topic %s: %w", r.topic, r.err)
			}
			continue
		}
		for k, v := range r.schemas {
			activeSchemas[k] = activeSchemas[k] | v
		}
	}

	if firstErr != nil {
		return nil, firstErr
	}
	return activeSchemas, nil
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

func validateFlags(cmd *cobra.Command, platformFlag, strategy, fromFile string, dryRun, softDelete, hardDelete, force bool, cpConfigFile, topicsFlag string, scanAllTopics bool) error {
	if fromFile != "" {
		if cmd.Flags().Changed("output") {
			return errors.New("--output and --from-file are mutually exclusive")
		}
		if dryRun {
			return errors.New("--dry-run and --from-file are mutually exclusive")
		}
		if !softDelete && !hardDelete {
			return errors.New("--from-file requires --soft-delete and/or --hard-delete")
		}
		// Reject flags that have no effect with --from-file
		for _, flag := range []string{"subject", "all", "topics", "scan-all-topics", "strategy", "context"} {
			if cmd.Flags().Changed(flag) {
				return fmt.Errorf("--%s has no effect with --from-file and cannot be combined", flag)
			}
		}
		return nil
	}

	// Standard mode validation
	if !cmd.Flags().Changed("all") && !cmd.Flags().Changed("subject") {
		return errors.New("at least one of --subject or --all must be specified")
	}
	if cmd.Flags().Changed("all") && cmd.Flags().Changed("subject") {
		return errors.New("only one of --subject or --all can be specified")
	}

	if cmd.Flags().Changed("subject") {
		subject, _ := cmd.Flags().GetString("subject")
		if strategy == "topic-name" && !pkg.VerifySubject(subject) {
			return errors.New("subject does not match TopicNameStrategy (must end with -key or -value)")
		}
	}

	if strategy == "record-name" && topicsFlag == "" && !scanAllTopics {
		return errors.New("--topics or --scan-all-topics is required with --strategy=record-name")
	}

	if platformFlag == "cp" && cpConfigFile == "" {
		return errors.New("--cp-config-file is required when --platform=cp")
	}

	if force && !softDelete && !hardDelete && !dryRun {
		// Force without explicit delete mode is fine — will default to soft+hard in interactive mode
	}

	return nil
}

func Execute() {
	var rootCmd = &cobra.Command{
		Use:   "confluent schema-registry cleanup",
		Short: "Schema deletion tool - a simple CLI to delete unused schemas",
		Long: `Schema deletion tool - a simple CLI to discover unused schemas from
Kafka topics and delete them from Schema Registry.

Supports both Confluent Cloud and Confluent Platform.`,
		RunE: run,
	}

	// Original flags
	rootCmd.Flags().StringP("subject", "V", "", "Subject to clean up schemas from.")
	rootCmd.Flags().Bool("all", false, "Clean up all eligible subjects.")
	rootCmd.Flags().String("config-file", "", "Path to config file containing credentials for Kafka clusters.")

	// Platform flags
	rootCmd.Flags().String("platform", "cloud", "Platform type: 'cloud' or 'cp' (Confluent Platform).")
	rootCmd.Flags().String("cp-config-file", "", "Path to CP config JSON file (required for --platform=cp).")

	// Strategy flags
	rootCmd.Flags().String("strategy", "topic-name", "Subject naming strategy: 'topic-name', 'record-name', 'topic-record-name'.")
	rootCmd.Flags().String("topics", "", "Comma-separated list of topics to scan (required for record-name strategy).")
	rootCmd.Flags().Bool("scan-all-topics", false, "Scan all topics across all clusters.")
	rootCmd.Flags().String("context", "", "Schema context to scope operations to (e.g., 'staging', 'production').")

	// Workflow flags
	rootCmd.Flags().Bool("dry-run", false, "Analyze candidates without deleting. Use with --output to save manifest.")
	rootCmd.Flags().String("output", "", "Path to write manifest file (implies --dry-run).")
	rootCmd.Flags().String("from-file", "", "Path to manifest file. Skips scanning, executes deletion directly.")
	rootCmd.Flags().Bool("soft-delete", false, "Execute soft-delete only.")
	rootCmd.Flags().Bool("hard-delete", false, "Execute hard-delete only (schemas must already be soft-deleted).")
	rootCmd.Flags().Bool("force", false, "Skip interactive confirmation prompts.")
	rootCmd.Flags().Int("workers", 25, "Number of concurrent topic scanners.")

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
