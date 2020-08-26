/*
2020 © Postgres.ai
*/

// Package logical provides jobs for logical initial operations.
package logical

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/pkg/errors"
	"github.com/sethvargo/go-password/password"

	dblabCfg "gitlab.com/postgres-ai/database-lab/pkg/config"
	"gitlab.com/postgres-ai/database-lab/pkg/log"
	"gitlab.com/postgres-ai/database-lab/pkg/retrieval/config"
	"gitlab.com/postgres-ai/database-lab/pkg/retrieval/dbmarker"
	"gitlab.com/postgres-ai/database-lab/pkg/retrieval/engine/postgres/tools"
	"gitlab.com/postgres-ai/database-lab/pkg/retrieval/engine/postgres/tools/defaults"
	"gitlab.com/postgres-ai/database-lab/pkg/retrieval/engine/postgres/tools/health"
	"gitlab.com/postgres-ai/database-lab/pkg/retrieval/options"
)

const (
	// RestoreJobType declares a job type for logical dumping.
	RestoreJobType = "logical-restore"

	// const defines restore options.
	restoreContainerPrefix = "dblab_lr_"
	defaultParallelJobs    = 1
)

// RestoreJob defines a logical restore job.
type RestoreJob struct {
	name         string
	dockerClient *client.Client
	globalCfg    *dblabCfg.Global
	dbMarker     *dbmarker.Marker
	dbMark       *dbmarker.Config
	RestoreOptions
}

// RestoreOptions defines a logical restore options.
type RestoreOptions struct {
	DumpFile     string  `yaml:"dumpLocation"`
	DockerImage  string  `yaml:"dockerImage"`
	DBName       string  `yaml:"dbname"`
	ForceInit    bool    `yaml:"forceInit"`
	ParallelJobs int     `yaml:"parallelJobs"`
	Partial      Partial `yaml:"partial"`
}

// Partial defines tables and rules for a partial logical restore.
type Partial struct {
	Tables []string `yaml:"tables"`
}

// NewJob create a new logical restore job.
func NewJob(cfg config.JobConfig, docker *client.Client, globalCfg *dblabCfg.Global, marker *dbmarker.Marker) (*RestoreJob, error) {
	restoreJob := &RestoreJob{
		name:         cfg.Name,
		dockerClient: docker,
		globalCfg:    globalCfg,
		dbMarker:     marker,
		dbMark:       &dbmarker.Config{DataType: dbmarker.LogicalDataType},
	}

	if err := options.Unmarshal(cfg.Options, &restoreJob.RestoreOptions); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal configuration options")
	}

	restoreJob.setDefaults()

	return restoreJob, nil
}

func (r *RestoreJob) setDefaults() {
	// TODO: Default yaml values in tags.
	if r.ParallelJobs == 0 {
		r.ParallelJobs = defaultParallelJobs
	}
}

func (r *RestoreJob) restoreContainerName() string {
	return restoreContainerPrefix + r.globalCfg.InstanceID
}

// Name returns a name of the job.
func (r *RestoreJob) Name() string {
	return r.name
}

// Run starts the job.
func (r *RestoreJob) Run(ctx context.Context) (err error) {
	log.Msg(fmt.Sprintf("Run job: %s. Options: %v", r.Name(), r.RestoreOptions))

	isEmpty, err := tools.IsEmptyDirectory(r.globalCfg.DataDir)
	if err != nil {
		return errors.Wrapf(err, "failed to explore the data directory %q", r.globalCfg.DataDir)
	}

	if !isEmpty {
		if !r.ForceInit {
			return errors.Errorf("the data directory %q is not empty. Use 'forceInit' or empty the data directory",
				r.globalCfg.DataDir)
		}

		log.Msg(fmt.Sprintf("The data directory %q is not empty. Existing data may be overwritten.", r.globalCfg.DataDir))
	}

	if err := tools.PullImage(ctx, r.dockerClient, r.RestoreOptions.DockerImage); err != nil {
		return errors.Wrap(err, "failed to scan image pulling response")
	}

	hostConfig, err := r.buildHostConfig()
	if err != nil {
		return errors.Wrap(err, "failed to build container host config")
	}

	pwd, err := password.Generate(tools.PasswordLength, tools.PasswordMinDigits, tools.PasswordMinSymbols, false, true)
	if err != nil {
		return errors.Wrap(err, "failed to generate PostgreSQL password")
	}

	cont, err := r.dockerClient.ContainerCreate(ctx,
		r.buildContainerConfig(pwd),
		hostConfig,
		&network.NetworkingConfig{},
		r.restoreContainerName(),
	)
	if err != nil {
		return errors.Wrapf(err, "failed to create container %q", r.restoreContainerName())
	}

	defer tools.RemoveContainer(ctx, r.dockerClient, cont.ID, tools.StopTimeout)

	defer func() {
		if err != nil {
			tools.PrintContainerLogs(ctx, r.dockerClient, r.restoreContainerName())
		}
	}()

	if err := r.dockerClient.ContainerStart(ctx, cont.ID, types.ContainerStartOptions{}); err != nil {
		return errors.Wrapf(err, "failed to start container %q", r.restoreContainerName())
	}

	log.Msg(fmt.Sprintf("Running container: %s. ID: %v", r.restoreContainerName(), cont.ID))

	if err := tools.CheckContainerReadiness(ctx, r.dockerClient, cont.ID); err != nil {
		return errors.Wrap(err, "failed to readiness check")
	}

	restoreCommand := r.buildLogicalRestoreCommand()
	log.Msg("Running restore command: ", restoreCommand)

	execCommand, err := r.dockerClient.ContainerExecCreate(ctx, cont.ID, types.ExecConfig{
		AttachStdout: true,
		AttachStderr: true,
		Tty:          true,
		Cmd:          restoreCommand,
	})

	if err != nil {
		return errors.Wrap(err, "failed to create restore command")
	}

	if len(r.Partial.Tables) > 0 {
		log.Msg("Partial restore will be run. Tables for restoring: ", strings.Join(r.Partial.Tables, ", "))
	}

	if err := r.dockerClient.ContainerExecStart(ctx, execCommand.ID, types.ExecStartCheck{Tty: true}); err != nil {
		return errors.Wrap(err, "failed to run restore command")
	}

	if err := tools.InspectCommandResponse(ctx, r.dockerClient, cont.ID, execCommand.ID); err != nil {
		return errors.Wrap(err, "failed to exec restore command")
	}

	if err := r.markDatabase(ctx, cont.ID); err != nil {
		return errors.Wrap(err, "failed to mark the database")
	}

	if err := recalculateStats(ctx, r.dockerClient, cont.ID, buildAnalyzeCommand(Connection{
		Username: defaults.Username,
		DBName:   r.RestoreOptions.DBName,
	}, r.RestoreOptions.ParallelJobs)); err != nil {
		return errors.Wrap(err, "failed to recalculate statistics after restore")
	}

	log.Msg("Restoring job has been finished")

	return nil
}

func (r *RestoreJob) buildContainerConfig(password string) *container.Config {
	return &container.Config{
		Labels: map[string]string{"label": tools.DBLabControlLabel},
		Env: append(os.Environ(), []string{
			"PGDATA=" + r.globalCfg.DataDir,
			"POSTGRES_PASSWORD=" + password,
		}...),
		Image:       r.RestoreOptions.DockerImage,
		Healthcheck: health.GetConfig(),
	}
}

func (r *RestoreJob) buildHostConfig() (*container.HostConfig, error) {
	hostConfig := &container.HostConfig{
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeBind,
				Source: r.RestoreOptions.DumpFile,
				Target: r.RestoreOptions.DumpFile,
			},
		},
	}

	if err := tools.AddVolumesToHostConfig(hostConfig, r.globalCfg.DataDir); err != nil {
		return nil, err
	}

	return hostConfig, nil
}

func (r *RestoreJob) markDatabase(ctx context.Context, contID string) error {
	dataStateAt, err := r.retrieveDataStateAt(ctx, contID)
	if err != nil {
		log.Err("Failed to extract dataStateAt: ", err)
	}

	if dataStateAt != "" {
		r.dbMark.DataStateAt = dataStateAt
	}

	if err := r.dbMarker.CreateConfig(); err != nil {
		return errors.Wrap(err, "failed to create a DBMarker config of the database")
	}

	if err := r.dbMarker.SaveConfig(r.dbMark); err != nil {
		return errors.Wrap(err, "failed to mark the database")
	}

	return nil
}

func (r *RestoreJob) retrieveDataStateAt(ctx context.Context, contID string) (string, error) {
	restoreMetaCmd := []string{"sh", "-c", "pg_restore --list " + r.RestoreOptions.DumpFile + " | head -n 10"}

	log.Dbg("Running a restore metadata command: ", restoreMetaCmd)

	execCommand, err := r.dockerClient.ContainerExecCreate(ctx, contID, types.ExecConfig{
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          restoreMetaCmd,
	})
	if err != nil {
		return "", errors.Wrap(err, "failed to create a restore metadata command")
	}

	execAttach, err := r.dockerClient.ContainerExecAttach(ctx, execCommand.ID, types.ExecStartCheck{})
	if err != nil {
		return "", errors.Wrap(err, "failed to exec a restore metadata command")
	}

	defer execAttach.Close()

	dataStateAt, err := tools.DiscoverDataStateAt(execAttach.Reader)
	if err != nil {
		return "", err
	}

	return dataStateAt, nil
}

func (r *RestoreJob) buildLogicalRestoreCommand() []string {
	restoreCmd := []string{"pg_restore", "--username", defaults.Username, "--dbname", defaults.DBName, "--create", "--no-privileges"}

	if r.ForceInit {
		restoreCmd = append(restoreCmd, "--clean", "--if-exists")
	}
	//else {
	//	restoreCmd = append(restoreCmd)
	//}

	restoreCmd = append(restoreCmd, "--jobs", strconv.Itoa(r.ParallelJobs))

	for _, table := range r.Partial.Tables {
		restoreCmd = append(restoreCmd, "--table", table)
	}

	restoreCmd = append(restoreCmd, r.RestoreOptions.DumpFile)

	return restoreCmd
}