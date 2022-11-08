package cmd

import (
	"os"

	"github.com/confluentinc/schema-deletion-tool/pkg"
	"github.com/spf13/cobra"
)

func run(cmd *cobra.Command, _ []string) error {
	var subjects []string
	subject, all, err := pkg.ValidateParams(cmd)
	if err != nil {
		return err
	}
	if all {
		subjects, err = pkg.GetAllEligibleSubjects()
		if err != nil {
			return err
		}
	} else {
		subjects = []string{subject}
	}

	topics := pkg.ExtractTopicFromSubject(subjects)

	// Don't display usage messages after parameters are validated.
	cmd.SilenceUsage = true

	configFile, err := cmd.Flags().GetString("config-file")
	if err != nil {
		return err
	}
	ctx, err := pkg.NewContext(configFile)
	if err != nil {
		return err
	}
	ctx.Subjects = subjects
	ctx.Topics = topics

	// Traverse all clusters in the current environment and prompt for credentials.
	err = pkg.ListClusters(ctx)
	if err != nil {
		return err
	}

	// Retrieve all schemas under the target subject(s).
	schemas, err := pkg.GetAllSchemas(ctx)
	if err != nil {
		return err
	}

	// Filter out all eligible topics and scan for active schemas.
	activeSchemas, err := pkg.ListAndScanTopics(ctx)
	if err != nil {
		return err
	}

	// Prompt users to delete schemas they want to soft/hard delete.
	selection, err := pkg.SelectDeletionCandidates(schemas, activeSchemas)
	if err != nil {
		return err
	}
	err = pkg.DeleteSchemas(selection)
	if err != nil {
		return err
	}
	return nil
}

func Execute() {
	var rootCmd = &cobra.Command{
		Use:   "confluent schema-registry cleanup",
		Short: "Schema deletion tool - a simple CLI to delete unused schemas",
		Long:  "Schema deletion tool - a simple CLI to discover unused schemas from user topics and delete them from Schema Registry",
		RunE:  run,
	}

	rootCmd.Flags().StringP("subject", "V", "", "Subject to clean up schemas from.")
	rootCmd.Flags().Bool("all", false, "Clean up all eligible subjects.")
	rootCmd.Flags().String("config-file", "", "Path to config file containing credentials for Kafka clusters.")

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
