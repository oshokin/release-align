package logger

import (
	"context"
	"strconv"
	"time"

	"go.uber.org/zap/zapcore"
)

// Progress counts one phase of a command and estimates what is left.
type Progress struct {
	// name is the phase printed on each line.
	name string
	// start is when this phase began.
	start time.Time
	// total is the number of units. Zero means the total is not known yet.
	total int
	// done is how many units have finished.
	done int
}

// NewProgress starts a phase. A non-positive total means the size is still unknown.
func NewProgress(name string, total int) *Progress {
	if total < 0 {
		total = 0
	}

	progress := &Progress{
		name:  name,
		start: time.Now(),
		total: total,
	}

	return progress
}

// Started is when this phase began.
func (p *Progress) Started() time.Time {
	if p == nil {
		return time.Time{}
	}

	return p.start
}

// Done is how many units have finished.
func (p *Progress) Done() int {
	if p == nil {
		return 0
	}

	return p.done
}

// Total is the phase size. Zero means the size is unknown.
func (p *Progress) Total() int {
	if p == nil {
		return 0
	}

	return p.total
}

// Start announces a phase before its first unit is attempted.
func (p *Progress) Start(ctx context.Context) {
	if p == nil {
		return
	}

	InfoKV(ctx, "Starting phase", p.fields("")...)
}

// Error records one failed unit. The caller owns the final operation error.
func (p *Progress) Error(ctx context.Context, repo, message string) {
	p.record(ctx, repo, message, zapcore.ErrorLevel)
}

// Advance records one finished unit and writes an info progress line.
// The repository name is colored when the destination is a terminal.
func (p *Progress) Advance(ctx context.Context, repo, message string) {
	p.record(ctx, repo, message, zapcore.InfoLevel)
}

// Warn records one finished unit and writes a warning progress line.
func (p *Progress) Warn(ctx context.Context, repo, message string) {
	p.record(ctx, repo, message, zapcore.WarnLevel)
}

// Finish writes the current counts without counting another unit.
func (p *Progress) Finish(ctx context.Context, message string) {
	if p == nil || p.done == 0 {
		return
	}

	InfoKV(ctx, message, p.fields("")...)
}

// record counts one completed or failed unit at the chosen severity.
func (p *Progress) record(ctx context.Context, repo, message string, level zapcore.Level) {
	if p == nil {
		p.write(ctx, message, level, "repo", repo)

		return
	}

	p.done++
	p.write(ctx, message, level, p.fields(repo)...)
}

// write sends one progress line at the chosen level.
func (*Progress) write(ctx context.Context, message string, level zapcore.Level, fields ...any) {
	switch level {
	case zapcore.ErrorLevel:
		ErrorKV(ctx, message, fields...)
	case zapcore.WarnLevel:
		WarnKV(ctx, message, fields...)
	default:
		InfoKV(ctx, message, fields...)
	}
}

// fields is the comma-separated tail of one progress line.
func (p *Progress) fields(repo string) []any {
	elapsed := time.Since(p.start)
	fields := make([]any, 0, 12)

	if repo != "" {
		fields = append(fields, "repo", repo)
	}

	fields = append(fields, "phase", p.name, "elapsed", DurationText(elapsed))

	if p.total <= 0 {
		return append(fields, "found", strconv.Itoa(p.done))
	}

	percent, left := PaceText(p.done, p.total, elapsed)

	return append(fields,
		"done", strconv.Itoa(p.done)+"/"+strconv.Itoa(p.total),
		"percent", percent,
		"left", left,
	)
}

// PaceText is the percent and the estimated remainder for one phase.
// left is unknown until one unit has finished.
func PaceText(done, total int, elapsed time.Duration) (percent, left string) {
	if total <= 0 {
		return "", ""
	}

	percent = percentText(done, total)
	if done <= 0 {
		return percent, "unknown"
	}

	if done >= total {
		return percent, DurationText(0)
	}

	remain := elapsed * time.Duration(total-done) / time.Duration(done)

	return percent, DurationText(remain)
}

// DurationText prints a rounded duration. Sub-second values stay in milliseconds.
func DurationText(d time.Duration) string {
	if d < 0 {
		d = 0
	}

	if d > 0 && d < time.Second {
		return d.Round(time.Millisecond).String()
	}

	return d.Round(time.Second).String()
}

// percentText prints 0% and values from 10% upward as integers, and the rest with one decimal.
func percentText(done, total int) string {
	value := float64(done) * 100 / float64(total)
	if value == 0 || value >= 10 {
		return strconv.Itoa(int(value)) + "%"
	}

	return strconv.FormatFloat(value, 'f', 1, 64) + "%"
}
