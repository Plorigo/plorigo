package agentcore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"connectrpc.com/connect"

	agentv1 "github.com/plorigo/plorigo/proto/gen/agent/v1"
	"github.com/plorigo/plorigo/proto/gen/agent/v1/agentv1connect"
)

// Backup statuses the agent reports, matching the agent.v1 protocol and the control plane's
// backups.Status* vocabulary.
const (
	backupStatusDumping    = "dumping"
	backupStatusVerifying  = "verifying"
	backupStatusSucceeded  = "succeeded"
	backupStatusFailed     = "failed"
	restoreStatusRestoring = "restoring"
)

const defaultBackupPollInterval = 10 * time.Second

// backupRuntime is the Docker surface the backup/restore loop needs: find a managed service's
// running container, run pg_dump inside it (streaming the dump to a writer), and restore a dump
// into it with psql (streaming from a reader). *dockerClient satisfies it (alongside
// deploymentRuntime).
type backupRuntime interface {
	findRunningByService(ctx context.Context, serviceID string) (containerID string, ok bool, err error)
	execPgDump(ctx context.Context, containerID, user, password, database string, dst io.Writer) error
	execPsqlRestore(ctx context.Context, containerID, user, password, database string, src io.Reader) error
}

// backupLoop polls the control plane for database-backup work and runs it until ctx is cancelled.
// It runs beside the heartbeat/deploy loops, reading the identity fresh on every call so it
// follows a runtime re-registration. Transport errors back off and retry; a failed backup
// (including Docker being unavailable) is reported, never fatal.
func backupLoop(ctx context.Context, out io.Writer, backup agentv1connect.BackupServiceClient, ident *identity, runtime backupRuntime, dataDir string, interval time.Duration) error {
	if interval <= 0 {
		interval = defaultBackupPollInterval
	}
	backoff := time.Second
	for {
		st := ident.get()
		resp, err := backup.PollBackupJob(ctx, connect.NewRequest(&agentv1.PollBackupJobRequest{
			AgentId:    st.AgentID,
			Credential: st.Credential,
		}))
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			fmt.Fprintf(out, "backup poll failed (retrying in %s): %v\n", backoff, err)
			if !sleep(ctx, backoff) {
				return nil
			}
			backoff = nextBackoff(backoff)
			continue
		}
		backoff = time.Second
		if !resp.Msg.GetHasWork() {
			// No backup queued — check for a restore before sleeping (one poll loop, two queues).
			if pollAndRunRestore(ctx, out, backup, ident, runtime) {
				continue
			}
			if !sleep(ctx, interval) {
				return nil
			}
			continue
		}
		executeBackup(ctx, out, backup, ident, runtime, dataDir, resp.Msg)
	}
}

// pollAndRunRestore claims and runs one restore if any is queued for this server, returning true
// when it did work (so the caller loops straight back). A transport error is logged and treated
// as "no work" — the next backup poll handles backoff.
func pollAndRunRestore(ctx context.Context, out io.Writer, backup agentv1connect.BackupServiceClient, ident *identity, runtime backupRuntime) bool {
	st := ident.get()
	resp, err := backup.PollRestoreJob(ctx, connect.NewRequest(&agentv1.PollRestoreJobRequest{
		AgentId:    st.AgentID,
		Credential: st.Credential,
	}))
	if err != nil {
		if ctx.Err() == nil {
			fmt.Fprintf(out, "restore poll failed: %v\n", err)
		}
		return false
	}
	if !resp.Msg.GetHasWork() {
		return false
	}
	executeRestore(ctx, out, backup, ident, runtime, resp.Msg)
	return true
}

// executeRestore runs one claimed restore: it finds the target Postgres container and pipes the
// backup artifact (from the server's disk) into psql inside it, reporting each transition. psql
// runs with ON_ERROR_STOP so a bad dump fails loudly rather than half-applying.
func executeRestore(ctx context.Context, out io.Writer, backup agentv1connect.BackupServiceClient, ident *identity, runtime backupRuntime, job *agentv1.PollRestoreJobResponse) {
	restoreID := job.GetRestoreId()
	report := func(status, message, errMsg string) {
		st := ident.get()
		if _, err := backup.ReportRestoreJob(ctx, connect.NewRequest(&agentv1.ReportRestoreJobRequest{
			AgentId:    st.AgentID,
			Credential: st.Credential,
			RestoreId:  restoreID,
			Status:     status,
			Message:    message,
			Error:      errMsg,
		})); err != nil {
			fmt.Fprintf(out, "restore report failed for %s: %v\n", restoreID, err)
		}
	}
	fail := func(msg string) { report(backupStatusFailed, "", msg) }

	if runtime == nil {
		fail("Docker is not available on this server")
		return
	}
	containerID, ok, err := runtime.findRunningByService(ctx, job.GetServiceId())
	if err != nil {
		fail("could not find the database container: " + err.Error())
		return
	}
	if !ok {
		fail("the database container is not running on this server")
		return
	}
	f, err := os.Open(job.GetArtifactUri())
	if err != nil {
		fail("could not open the backup artifact: " + err.Error())
		return
	}
	defer func() { _ = f.Close() }()

	report(restoreStatusRestoring, "restoring with psql", "")
	if err := runtime.execPsqlRestore(ctx, containerID, job.GetPgUser(), job.GetPgPassword(), job.GetPgDatabase(), f); err != nil {
		fail("restore failed: " + err.Error())
		return
	}
	report(backupStatusSucceeded, "restore complete", "")
	fmt.Fprintf(out, "restore %s succeeded\n", restoreID)
}

// executeBackup runs one claimed backup: it finds the managed Postgres container, runs pg_dump
// inside it, streams the dump to a 0700 agent-owned file on the server, computes its size and
// sha256, verifies the file, and reports each transition. Any step can fail; on failure the
// partial artifact is removed.
func executeBackup(ctx context.Context, out io.Writer, backup agentv1connect.BackupServiceClient, ident *identity, runtime backupRuntime, dataDir string, job *agentv1.PollBackupJobResponse) {
	backupID := job.GetBackupId()
	report := func(status, artifactURI string, size int64, checksum, message, errMsg string) {
		st := ident.get()
		if _, err := backup.ReportBackupJob(ctx, connect.NewRequest(&agentv1.ReportBackupJobRequest{
			AgentId:     st.AgentID,
			Credential:  st.Credential,
			BackupId:    backupID,
			Status:      status,
			ArtifactUri: artifactURI,
			SizeBytes:   size,
			Checksum:    checksum,
			Message:     message,
			Error:       errMsg,
		})); err != nil {
			fmt.Fprintf(out, "backup report failed for %s: %v\n", backupID, err)
		}
	}
	fail := func(msg string) { report(backupStatusFailed, "", 0, "", "", msg) }

	if runtime == nil {
		fail("Docker is not available on this server")
		return
	}
	containerID, ok, err := runtime.findRunningByService(ctx, job.GetServiceId())
	if err != nil {
		fail("could not find the database container: " + err.Error())
		return
	}
	if !ok {
		fail("the database container is not running on this server")
		return
	}

	report(backupStatusDumping, "", 0, "", "running pg_dump", "")

	// Write the dump to a 0700 agent-owned directory on the server's own disk (the MVP "local"
	// destination). The data never leaves the user's machine; an S3 destination is a later slice.
	dir := filepath.Join(dataDir, "backups", job.GetServiceId())
	if err := os.MkdirAll(dir, 0o700); err != nil {
		fail("could not create the backup directory: " + err.Error())
		return
	}
	path := filepath.Join(dir, backupID+".sql")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		fail("could not create the backup file: " + err.Error())
		return
	}
	hasher := sha256.New()
	counter := &countingWriter{}
	dumpErr := runtime.execPgDump(ctx, containerID, job.GetPgUser(), job.GetPgPassword(), job.GetPgDatabase(), io.MultiWriter(f, hasher, counter))
	closeErr := f.Close()
	if dumpErr != nil {
		_ = os.Remove(path)
		fail("pg_dump failed: " + dumpErr.Error())
		return
	}
	if closeErr != nil {
		_ = os.Remove(path)
		fail("could not finalize the backup file: " + closeErr.Error())
		return
	}
	if counter.n == 0 {
		_ = os.Remove(path)
		fail("pg_dump produced no output")
		return
	}

	checksum := hex.EncodeToString(hasher.Sum(nil))
	report(backupStatusVerifying, path, counter.n, checksum, "verifying the backup", "")
	if info, err := os.Stat(path); err != nil || info.Size() != counter.n {
		fail("backup verification failed: the artifact is missing or truncated")
		return
	}
	report(backupStatusSucceeded, path, counter.n, checksum, fmt.Sprintf("backed up %d bytes", counter.n), "")
	fmt.Fprintf(out, "backup %s succeeded (%d bytes -> %s)\n", backupID, counter.n, path)
}

// countingWriter counts the bytes written through it, so the backup loop records the artifact size
// while streaming the dump (without buffering it in memory).
type countingWriter struct{ n int64 }

func (c *countingWriter) Write(p []byte) (int, error) {
	c.n += int64(len(p))
	return len(p), nil
}
