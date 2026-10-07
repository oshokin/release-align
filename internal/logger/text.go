package logger

import (
	"bytes"
	"fmt"
	"slices"
	"time"

	"go.uber.org/zap/zapcore"
)

// textCore writes one human-readable log line.
// A newline inside the message starts another line, so a file list can sit in a column.
type textCore struct {
	// level is the minimum severity this core records.
	level zapcore.LevelEnabler
	// out receives the finished line.
	out zapcore.WriteSyncer
	// color paints the level word and the repository name.
	color bool
	// fields are the context values inherited from With.
	fields []zapcore.Field
}

// Enabled reports whether level is recorded.
func (c *textCore) Enabled(level zapcore.Level) bool {
	return c.level.Enabled(level)
}

// With returns a core that keeps fields on later lines.
func (c *textCore) With(fields []zapcore.Field) zapcore.Core {
	copied := c.clone()
	copied.fields = append(copied.fields, fields...)

	return &copied
}

// Check adds this core when the entry level is enabled.
//
//nolint:gocritic // hugeParam: zapcore.Core.Check requires Entry by value.
func (c *textCore) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(ent.Level) {
		return ce.AddCore(ent, c)
	}

	return ce
}

// Write formats one text line, including any extra lines inside the message.
//
//nolint:gocritic // hugeParam: zapcore.Core.Write requires Entry by value.
func (c *textCore) Write(ent zapcore.Entry, fields []zapcore.Field) error {
	all := slices.Concat(c.fields, fields)
	_, err := c.out.Write(c.formatLine(ent.Time, ent.Level, ent.Message, all))

	return err
}

// Sync does not fsync the destination.
func (c *textCore) Sync() error {
	return nil
}

// clone copies the core and its fields. The writer and the level stay the same sink.
func (c *textCore) clone() textCore {
	return textCore{
		level:  c.level,
		out:    c.out,
		color:  c.color,
		fields: slices.Clone(c.fields),
	}
}

// newTextCore builds a locked text core.
func newTextCore(level zapcore.LevelEnabler, out zapcore.WriteSyncer, color bool) zapcore.Core {
	return &textCore{level: level, out: zapcore.Lock(out), color: color}
}

// formatLine renders time, level, repository, message, and any other fields.
func (c *textCore) formatLine(when time.Time, level zapcore.Level, message string, fields []zapcore.Field) []byte {
	repo, rest := c.splitRepo(fields)

	var b bytes.Buffer

	b.WriteString(when.Format(timeLayout))
	b.WriteString("  ")
	c.writeTint(&b, c.padLevel(level), c.levelStyle(level.String()))
	b.WriteString("  ")

	if repo != "" {
		c.writeTint(&b, repo, c.repoColor(repo))
		b.WriteString("  ")
	}

	b.WriteString(message)
	c.writeFields(&b, rest)
	b.WriteByte('\n')

	return b.Bytes()
}

// padLevel prints a level in a fixed column.
func (c *textCore) padLevel(level zapcore.Level) string {
	return fmt.Sprintf("%-*s", levelWidth, level.CapitalString())
}

// splitRepo pulls the repository name out of the context fields.
func (c *textCore) splitRepo(fields []zapcore.Field) (string, []zapcore.Field) {
	repo := ""
	rest := make([]zapcore.Field, 0, len(fields))

	for _, field := range fields {
		if field.Key == fieldRepo && field.Type == zapcore.StringType {
			repo = field.String

			continue
		}

		rest = append(rest, field)
	}

	return repo, rest
}

// writeFields appends leftover context as key=value pairs.
func (c *textCore) writeFields(b *bytes.Buffer, fields []zapcore.Field) {
	for _, field := range fields {
		b.WriteString("  ")
		b.WriteString(field.Key)
		b.WriteByte('=')
		b.WriteString(c.fieldText(field))
	}
}

// fieldText renders one zap field.
func (c *textCore) fieldText(field zapcore.Field) string {
	if field.Type == zapcore.StringType {
		return field.String
	}

	enc := zapcore.NewMapObjectEncoder()
	field.AddTo(enc)

	value, ok := enc.Fields[field.Key]
	if !ok {
		return ""
	}

	return fmt.Sprint(value)
}

// writeTint writes text, optionally wrapped in an ANSI color.
func (c *textCore) writeTint(b *bytes.Buffer, text, color string) {
	if !c.color || color == "" {
		b.WriteString(text)

		return
	}

	b.WriteString(color)
	b.WriteString(text)
	b.WriteString(ansiReset)
}
