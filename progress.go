package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gechr/clib/human"
)

type gitCallback interface {
	Progress(p *gitProgress)
	LocalSideband(message string, term *sidebandTerminator)
	RemoteSideband(message string, term *sidebandTerminator)
}

type sidebandTerminator byte

const (
	sidebandCR sidebandTerminator = '\r'
	sidebandLF sidebandTerminator = '\n'
)

type phaseProgress struct {
	Current int
	Total   int
}

type transferStats struct {
	Bytes float64 // bytes received so far
	Speed float64 // bytes per second
}

type gitProgress struct {
	Counted       phaseProgress
	Compressed    phaseProgress
	Objects       phaseProgress
	Deltas        phaseProgress
	Files         phaseProgress
	FilterContent phaseProgress
	Transfer      transferStats
	Transferring  bool
}

type lfsProgress struct {
	Operation    string
	CurrentFile  int
	TotalFiles   int
	CurrentBytes int64
	TotalBytes   int64
	Name         string
}

type cloneProgress struct {
	Git gitProgress
	LFS lfsProgress
}

const (
	cloneDisplayTotal = 1000
	progressPercent   = 100
	countWeight       = 100
	compressWeight    = 100
	receiveWeight     = 500
	deltasWeight      = 100
	filesWeight       = 100
	checkoutWeight    = 100
	lfsReadBufferSize = 4096
	lfsPollInterval   = 100 * time.Millisecond
)

func (p *gitProgress) apply(line string) bool {
	switch {
	case strings.HasPrefix(line, "remote: Counting objects:"):
		return p.updatePhase(
			&p.Counted,
			strings.TrimPrefix(line, "remote: Counting objects:"),
			false,
		)
	case strings.HasPrefix(line, "remote: Compressing objects:"):
		return p.updatePhase(
			&p.Compressed,
			strings.TrimPrefix(line, "remote: Compressing objects:"),
			false,
		)
	case strings.HasPrefix(line, "Receiving objects:"):
		return p.updatePhase(&p.Objects, strings.TrimPrefix(line, "Receiving objects:"), true)
	case strings.HasPrefix(line, "Resolving deltas:"):
		return p.updatePhase(&p.Deltas, strings.TrimPrefix(line, "Resolving deltas:"), false)
	case strings.HasPrefix(line, "Updating files:"):
		return p.updatePhase(&p.Files, strings.TrimPrefix(line, "Updating files:"), false)
	case strings.HasPrefix(line, "Filtering content:"):
		return p.updatePhase(
			&p.FilterContent,
			strings.TrimPrefix(line, "Filtering content:"),
			false,
		)
	default:
		return false
	}
}

func (p *gitProgress) updatePhase(phase *phaseProgress, line string, trackTransfer bool) bool {
	if current, total, ok := readProgressCounts(line); ok {
		phase.Current = current
		phase.Total = total
	}
	p.Transferring = trackTransfer
	if trackTransfer {
		p.Transfer = readTransferStats(line)
	}
	return true
}

func (p *gitProgress) Current() int {
	return p.Counted.Current + p.Compressed.Current + p.Objects.Current + p.Deltas.Current + p.Files.Current + p.FilterContent.Current
}

func (p *gitProgress) Total() int {
	return p.Counted.Total + p.Compressed.Total + p.Objects.Total + p.Deltas.Total + p.Files.Total + p.FilterContent.Total
}

func (p *gitProgress) Overall() float64 {
	t := p.Total()
	if t == 0 {
		return 0
	}
	return float64(p.Current()) / float64(t)
}

func (p cloneProgress) DisplayCurrent() int {
	switch {
	case p.LFS.TotalFiles > 0:
		return countWeight +
			compressWeight +
			receiveWeight +
			deltasWeight +
			filesWeight +
			displayLFSPhaseValue(p.LFS, checkoutWeight)
	case p.Git.FilterContent.Total > 0:
		return countWeight +
			compressWeight +
			receiveWeight +
			deltasWeight +
			filesWeight +
			displayPhaseValue(p.Git.FilterContent, checkoutWeight)
	case p.Git.Files.Total > 0:
		return countWeight +
			compressWeight +
			receiveWeight +
			deltasWeight +
			displayPhaseValue(p.Git.Files, filesWeight)
	case p.Git.Deltas.Total > 0:
		return countWeight +
			compressWeight +
			receiveWeight +
			displayPhaseValue(p.Git.Deltas, deltasWeight)
	case p.Git.Objects.Total > 0:
		return countWeight +
			compressWeight +
			displayPhaseValue(p.Git.Objects, receiveWeight)
	case p.Git.Compressed.Total > 0:
		return countWeight + displayPhaseValue(p.Git.Compressed, compressWeight)
	case p.Git.Counted.Total > 0:
		return displayPhaseValue(p.Git.Counted, countWeight)
	default:
		return 0
	}
}

func (p cloneProgress) DisplayTotal() int {
	total := cloneDisplayTotal
	if p.Git.Files.Total == 0 {
		total -= filesWeight
	}
	if !p.HasCheckoutProgress() {
		total -= checkoutWeight
	}
	return total
}

func (p cloneProgress) DisplayState(lastProgress int) (int, int) {
	total := p.DisplayTotal()
	current := max(p.DisplayCurrent(), lastProgress)
	if total > 1 && p.ShouldParkAtPending() {
		current = pendingProgressValue(total)
	} else if total > 1 && current >= total {
		current = pendingProgressValue(total)
	}
	return current, total
}

func (p cloneProgress) Message() string {
	switch {
	case p.LFS.TotalFiles > 0:
		switch p.LFS.Operation {
		case "download":
			return "Downloading LFS"
		default:
			return "Checking out LFS"
		}
	case p.Git.FilterContent.Total > 0:
		return "Checking out LFS"
	case p.Git.Files.Total > 0 && p.Git.Files.Current == p.Git.Files.Total:
		return "Checking out"
	case p.Git.Deltas.Total > 0 && p.Git.Deltas.Current == p.Git.Deltas.Total:
		return "Checking out"
	default:
		return "Cloning"
	}
}

func (p cloneProgress) HasCheckoutProgress() bool {
	return p.LFS.TotalFiles > 0 || p.Git.FilterContent.Total > 0
}

func (p cloneProgress) ShouldParkAtPending() bool {
	return !p.HasCheckoutProgress() &&
		p.Git.Deltas.Total > 0 &&
		p.Git.Deltas.Current == p.Git.Deltas.Total
}

func pendingProgressValue(total int) int {
	return total - max(1, total/progressPercent)
}

func displayPhaseValue(phase phaseProgress, weight int) int {
	if phase.Total <= 0 || weight <= 0 {
		return 0
	}

	current := min(max(phase.Current, 0), phase.Total)

	return current * weight / phase.Total
}

func displayLFSPhaseValue(progress lfsProgress, weight int) int {
	if progress.TotalFiles <= 0 || weight <= 0 {
		return 0
	}

	totalFiles := max(progress.TotalFiles, 1)
	currentFile := min(max(progress.CurrentFile, 1), totalFiles)
	completedFiles := currentFile - 1

	numerator := int64(completedFiles)
	denominator := int64(totalFiles)
	if progress.TotalBytes > 0 {
		currentBytes := min(max(progress.CurrentBytes, 0), progress.TotalBytes)
		numerator = numerator*progress.TotalBytes + currentBytes
		denominator *= progress.TotalBytes
	}

	return int(numerator * int64(weight) / denominator)
}

func relayGitProgress(src io.Reader, cb gitCallback) (string, error) {
	reader := bufio.NewReader(src)
	progress := gitProgress{}
	var data bytes.Buffer

	for {
		line, term, err := readSidebandLine(reader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return data.String(), err
		}
		if line == "" {
			continue
		}

		if isErrorLine(line) {
			data.WriteString(line)
			remaining, _ := io.ReadAll(reader)
			if len(remaining) > 0 {
				data.WriteByte('\n')
				data.Write(remaining)
			}
			break
		}

		if progress.apply(line) {
			cb.Progress(&progress)
			continue
		}

		if message, ok := strings.CutPrefix(line, "remote: "); ok {
			cb.RemoteSideband(message, term)
		} else {
			cb.LocalSideband(line, term)
		}
	}

	return data.String(), nil
}

func relayLFSProgress(ctx context.Context, path string, fn func(*lfsProgress)) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	buffer := make([]byte, lfsReadBufferSize)
	var pending strings.Builder
	ticker := time.NewTicker(lfsPollInterval)
	defer ticker.Stop()

	for {
		n, err := file.Read(buffer)
		if n > 0 {
			pending.Write(buffer[:n])
			for {
				line, rest, ok := strings.Cut(pending.String(), "\n")
				if !ok {
					break
				}
				pending.Reset()
				pending.WriteString(rest)

				line = strings.TrimSuffix(line, "\r")
				if progress, ok := readLFSProgress(line); ok {
					fn(&progress)
				}
			}
		}

		switch {
		case err == nil:
			continue
		case errors.Is(err, io.EOF):
			select {
			case <-ctx.Done():
				if progress, ok := readLFSProgress(strings.TrimSpace(pending.String())); ok {
					fn(&progress)
				}
				return nil
			case <-ticker.C:
				continue
			}
		default:
			return err
		}
	}
}

func isErrorLine(line string) bool {
	return strings.HasPrefix(line, "error: ") || strings.HasPrefix(line, "fatal: ")
}

func readSidebandLine(reader *bufio.Reader) (string, *sidebandTerminator, error) {
	raw, err := readUntilCRLF(reader)
	if err != nil {
		return "", nil, err
	}
	line, term := trimSidebandLine(raw)
	return line, term, nil
}

func readProgressCounts(line string) (int, int, bool) {
	open := strings.IndexByte(line, '(')
	closeIdx := strings.IndexByte(line, ')')
	if open < 0 || closeIdx <= open+1 {
		return 0, 0, false
	}

	counts := line[open+1 : closeIdx]
	currentText, totalText, ok := strings.Cut(counts, "/")
	if !ok {
		return 0, 0, false
	}

	current, err := strconv.Atoi(strings.TrimSpace(currentText))
	if err != nil {
		return 0, 0, false
	}
	total, err := strconv.Atoi(strings.TrimSpace(totalText))
	if err != nil || current > total {
		return 0, 0, false
	}

	return current, total, true
}

// readTransferStats extracts transfer information after the closing paren in a
// git progress line. Git formats throughput as:
//
//	", 27.61 MiB | 13.58 MiB/s"
//
// See git/progress.c throughput_string() for the canonical format.
func readTransferStats(line string) transferStats {
	closeIdx := strings.IndexByte(line, ')')
	if closeIdx < 0 || closeIdx+1 >= len(line) {
		return transferStats{}
	}
	rest := strings.TrimLeft(line[closeIdx+1:], ", ")
	rest = strings.TrimSuffix(rest, ", done.")
	rest = strings.TrimSuffix(rest, "done.")
	rest = strings.TrimRight(rest, ", ")
	if rest == "" {
		return transferStats{}
	}

	// Split on " | " to get bytes and speed parts.
	// Format: "27.61 MiB | 13.58 MiB/s"
	bytesStr, speedStr, hasSpeed := strings.Cut(rest, " | ")

	var stats transferStats
	stats.Bytes = human.ParseByteSize(strings.TrimSpace(bytesStr))
	if hasSpeed {
		stats.Speed = human.ParseByteSize(strings.TrimSuffix(strings.TrimSpace(speedStr), "/s"))
	}
	return stats
}

func readLFSProgress(line string) (lfsProgress, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return lfsProgress{}, false
	}

	operation, rest, ok := cutProgressToken(line)
	if !ok {
		return lfsProgress{}, false
	}
	fileCounts, rest, ok := cutProgressToken(rest)
	if !ok {
		return lfsProgress{}, false
	}
	byteCounts, name, ok := cutProgressToken(rest)
	if !ok {
		return lfsProgress{}, false
	}

	currentFile, totalFiles, ok := readProgressCountsPair(fileCounts)
	if !ok {
		return lfsProgress{}, false
	}
	currentBytes, totalBytes, ok := readProgressBytesPair(byteCounts)
	if !ok {
		return lfsProgress{}, false
	}

	return lfsProgress{
		Operation:    operation,
		CurrentFile:  currentFile,
		TotalFiles:   totalFiles,
		CurrentBytes: currentBytes,
		TotalBytes:   totalBytes,
		Name:         name,
	}, true
}

func cutProgressToken(line string) (string, string, bool) {
	line = strings.TrimLeft(line, " ")
	if line == "" {
		return "", "", false
	}

	before, after, ok := strings.Cut(line, " ")
	if !ok {
		return line, "", false
	}

	return before, after, true
}

func readProgressCountsPair(value string) (int, int, bool) {
	currentText, totalText, ok := strings.Cut(value, "/")
	if !ok {
		return 0, 0, false
	}

	current, err := strconv.Atoi(currentText)
	if err != nil {
		return 0, 0, false
	}
	total, err := strconv.Atoi(totalText)
	if err != nil || total <= 0 || current <= 0 || current > total {
		return 0, 0, false
	}

	return current, total, true
}

func readProgressBytesPair(value string) (int64, int64, bool) {
	currentText, totalText, ok := strings.Cut(value, "/")
	if !ok {
		return 0, 0, false
	}

	current, err := strconv.ParseInt(currentText, 10, 64)
	if err != nil {
		return 0, 0, false
	}
	total, err := strconv.ParseInt(totalText, 10, 64)
	if err != nil || total <= 0 || current < 0 || current > total {
		return 0, 0, false
	}

	return current, total, true
}

func readUntilCRLF(reader *bufio.Reader) (string, error) {
	var data bytes.Buffer

	for {
		b, err := reader.ReadByte()
		if err != nil {
			if err == io.EOF && data.Len() > 0 {
				return data.String(), nil
			}
			return "", err
		}

		data.WriteByte(b)
		if b == '\r' || b == '\n' {
			return data.String(), nil
		}
	}
}

func trimSidebandLine(line string) (string, *sidebandTerminator) {
	var term *sidebandTerminator
	switch {
	case strings.HasSuffix(line, "\r"):
		t := sidebandCR
		term = &t
		line = strings.TrimSuffix(line, "\r")
	case strings.HasSuffix(line, "\n"):
		t := sidebandLF
		term = &t
		line = strings.TrimSuffix(line, "\n")
	}
	return strings.TrimRight(line, " "), term
}
