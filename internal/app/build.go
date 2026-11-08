package app

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/ymotongpoo/reviewer/internal/store"
)

const (
	buildTimeout = 30 * time.Minute
	maxBuildLog  = 64 << 10
	maxArtifact  = 250 << 20
	maxArtifacts = 20
)

type cappedBuffer struct {
	mu sync.Mutex
	bytes.Buffer
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	left := maxBuildLog - b.Len()
	if left > 0 {
		if left > len(p) {
			left = len(p)
		}
		_, _ = b.Buffer.Write(p[:left])
	}
	return n, nil
}

// runPostAgentBuild invokes the configured session command after a successful
// agent run and saves matching output files in that run's round.
func (a *App) runPostAgentBuild(round int) {
	if a.SessionInfo().BuildCommand == "" {
		return
	}
	if _, err := a.runBuild(context.Background(), round); err != nil {
		log.Printf("run round %d post-agent build: %v", round, err)
	}
}

// RebuildRound reruns the configured command for an existing round.
func (a *App) RebuildRound(ctx context.Context, round int) (*store.BuildRecord, error) {
	if round < 1 {
		return nil, badRequest("ラウンド番号が不正です")
	}
	if _, err := a.Store.Manifest(round); err != nil {
		return nil, notFound("ラウンド%dがありません", round)
	}
	if a.hasRunningAgent() {
		return nil, conflict("エージェントの対応中は再ビルドできません")
	}
	return a.runBuild(ctx, round)
}

func (a *App) hasRunningAgent() bool {
	a.agents.mu.Lock()
	defer a.agents.mu.Unlock()
	for _, active := range a.agents.active {
		if active.rec.EndedAt == nil {
			return true
		}
	}
	return false
}

func (a *App) buildRunning(round int) bool {
	a.buildMu.Lock()
	defer a.buildMu.Unlock()
	return a.buildingBuilds[round]
}

func (a *App) runBuild(parent context.Context, round int) (*store.BuildRecord, error) {
	a.buildMu.Lock()
	if a.buildingBuilds == nil {
		a.buildingBuilds = make(map[int]bool)
	}
	if a.buildingBuilds[round] {
		a.buildMu.Unlock()
		return nil, conflict("ラウンド%dのビルドはすでに実行中です", round)
	}
	a.buildingBuilds[round] = true
	a.buildMu.Unlock()
	defer func() {
		a.buildMu.Lock()
		delete(a.buildingBuilds, round)
		a.buildMu.Unlock()
	}()

	a.mu.Lock()
	session := a.Session
	a.mu.Unlock()
	if session.BuildCommand == "" {
		return nil, badRequest("このセッションにビルドコマンドが設定されていません")
	}
	started := a.now()
	record := &store.BuildRecord{
		Round: round, Command: session.BuildCommand, Directory: session.BuildDir,
		Status: "running", StartedAt: started, ExitCode: -1,
	}
	if err := a.Store.SaveBuildRecord(record); err != nil {
		return nil, err
	}

	root := a.Proj.Root
	cwd := root
	if session.BuildDir != "" {
		if filepath.IsAbs(session.BuildDir) {
			cwd = filepath.Clean(session.BuildDir)
		} else {
			cwd = filepath.Join(root, session.BuildDir)
		}
	}
	pattern := session.BuildArtifact
	if pattern != "" && !filepath.IsAbs(pattern) {
		pattern = filepath.Join(root, pattern)
	}
	before, globErr := buildArtifactSnapshot(pattern)
	if globErr != nil {
		record.Status, record.Error, record.EndedAt = "failed", globErr.Error(), a.now()
		if err := a.Store.SaveBuildRecord(record); err != nil {
			log.Printf("save round %d build result: %v", round, err)
		}
		if a.notify != nil {
			a.notify(Event{Type: "round", Round: round})
		}
		return record, nil
	}
	ctx, cancel := context.WithTimeout(parent, buildTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", session.BuildCommand)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), fmt.Sprintf("REVIEWER_ROUND=%d", round), "REVIEWER_PROJECT_ROOT="+root)
	var output cappedBuffer
	cmd.Stdout, cmd.Stderr = &output, &output
	err := cmd.Run()
	record.Output = output.String()
	if output.Len() == maxBuildLog {
		record.Output += "\n[ログを64 KiBで省略しました]"
	}
	if err != nil {
		record.Status = "failed"
		record.Error = err.Error()
		if ctx.Err() != nil {
			record.Error = "コマンドが30分でタイムアウトしました"
		}
		if exit, ok := err.(*exec.ExitError); ok {
			record.ExitCode = exit.ExitCode()
		}
	} else {
		record.ExitCode = 0
		if pattern == "" {
			record.Status, record.Error = "failed", "成果物の glob が設定されていません"
		} else {
			matches, globErr := filepath.Glob(pattern)
			if globErr != nil {
				record.Status, record.Error = "failed", globErr.Error()
			}
			for _, path := range matches {
				if len(record.Artifacts) >= maxArtifacts {
					record.Status, record.Error = "failed", fmt.Sprintf("成果物は%dファイルまでです", maxArtifacts)
					break
				}
				info, statErr := os.Stat(path)
				if statErr != nil || !info.Mode().IsRegular() {
					continue
				}
				if previous, existed := before[path]; existed && previous.sameFile(info) {
					continue
				}
				if info.Size() > maxArtifact {
					record.Status, record.Error = "failed", fmt.Sprintf("成果物 %s は250 MiBを超えています", filepath.Base(path))
					continue
				}
				data, readErr := os.ReadFile(path)
				if readErr != nil {
					record.Status, record.Error = "failed", readErr.Error()
					continue
				}
				artifact, putErr := a.Store.PutBuildArtifact(round, filepath.Base(path), data, a.now())
				if putErr != nil {
					record.Status, record.Error = "failed", putErr.Error()
					continue
				}
				record.Artifacts = append(record.Artifacts, artifact)
			}
			if record.Error == "" && len(record.Artifacts) == 0 {
				record.Status, record.Error = "failed", "今回のビルドで新規作成または更新された成果物がありませんでした"
			}
			if record.Error == "" {
				record.Status = "completed"
			}
		}
	}
	record.EndedAt = a.now()
	if err := a.Store.SaveBuildRecord(record); err != nil {
		log.Printf("save round %d build result: %v", round, err)
	}
	if a.notify != nil {
		a.notify(Event{Type: "round", Round: round})
	}
	return record, nil
}

type artifactFingerprint struct {
	size    int64
	modTime time.Time
	file    os.FileInfo
}

func fingerprint(info os.FileInfo) artifactFingerprint {
	return artifactFingerprint{size: info.Size(), modTime: info.ModTime(), file: info}
}

func (f artifactFingerprint) sameFile(info os.FileInfo) bool {
	return f.size == info.Size() && f.modTime.Equal(info.ModTime()) && os.SameFile(f.file, info)
}

func buildArtifactSnapshot(pattern string) (map[string]artifactFingerprint, error) {
	snapshot := make(map[string]artifactFingerprint)
	if pattern == "" {
		return snapshot, nil
	}
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	for _, path := range matches {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		snapshot[path] = fingerprint(info)
	}
	return snapshot, nil
}

// RoundArtifact returns a stored artifact only when it belongs to this round.
func (a *App) RoundArtifact(round int, id string) ([]byte, string, string, error) {
	data, mediaType, err := a.Store.ReadBuildArtifact(round, id)
	if err != nil {
		return nil, "", "", notFound("成果物がありません")
	}
	record, err := a.Store.BuildRecord(round)
	if err != nil {
		return nil, "", "", notFound("成果物がありません")
	}
	for _, artifact := range record.Artifacts {
		if artifact.ID == id {
			return data, mediaType, artifact.Name, nil
		}
	}
	return nil, "", "", notFound("成果物がありません")
}
