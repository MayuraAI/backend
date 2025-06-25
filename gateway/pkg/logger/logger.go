package logger

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"runtime"
	"time"
)

// LogLevel represents the severity level of a log entry
type LogLevel string

const (
	DEBUG LogLevel = "DEBUG"
	INFO  LogLevel = "INFO"
	WARN  LogLevel = "WARN"
	ERROR LogLevel = "ERROR"
)

// LogEntry represents a structured log entry
type LogEntry struct {
	Timestamp string                 `json:"timestamp"`
	Level     LogLevel               `json:"level"`
	Service   string                 `json:"service"`
	Logger    string                 `json:"logger"`
	Message   string                 `json:"message"`
	RequestID string                 `json:"request_id,omitempty"`
	Fields    map[string]interface{} `json:"fields,omitempty"`
	Error     string                 `json:"error,omitempty"`
	Stack     string                 `json:"stack,omitempty"`
}

// Logger represents a structured logger instance
type Logger struct {
	name      string
	level     LogLevel
	service   string
	formatter Formatter
}

// Formatter interface for log formatting
type Formatter interface {
	Format(entry LogEntry) string
}

// JSONFormatter formats logs as JSON
type JSONFormatter struct{}

func (f *JSONFormatter) Format(entry LogEntry) string {
	data, _ := json.Marshal(entry)
	return string(data)
}

// TextFormatter formats logs as human-readable text
type TextFormatter struct{}

func (f *TextFormatter) Format(entry LogEntry) string {
	timestamp := entry.Timestamp
	level := entry.Level
	service := entry.Service
	logger := entry.Logger
	message := entry.Message
	requestID := entry.RequestID

	if requestID != "" {
		return fmt.Sprintf("%s [%s] %s.%s [%s] %s", timestamp, level, service, logger, requestID, message)
	}
	return fmt.Sprintf("%s [%s] %s.%s %s", timestamp, level, service, logger, message)
}

// Global configuration
var (
	defaultLogger *Logger
	globalLevel   LogLevel = INFO
	useJSON       bool     = false
)

// Initialize sets up the default logger
func Initialize(service string, level LogLevel, jsonFormat bool) {
	globalLevel = level
	useJSON = jsonFormat

	var formatter Formatter
	if jsonFormat {
		formatter = &JSONFormatter{}
	} else {
		formatter = &TextFormatter{}
	}

	defaultLogger = &Logger{
		name:      "default",
		level:     level,
		service:   service,
		formatter: formatter,
	}
}

// GetLogger creates a new logger instance
func GetLogger(name string) *Logger {
	if defaultLogger == nil {
		Initialize("gateway", INFO, false)
	}

	var formatter Formatter
	if useJSON {
		formatter = &JSONFormatter{}
	} else {
		formatter = &TextFormatter{}
	}

	return &Logger{
		name:      name,
		level:     globalLevel,
		service:   defaultLogger.service,
		formatter: formatter,
	}
}

// Context key for request ID
type contextKey string

const RequestIDKey contextKey = "request_id"

// GenerateRequestID generates a unique request ID
func GenerateRequestID() string {
	return fmt.Sprintf("%08x", rand.Uint32())
}

// WithRequestID adds a request ID to the context
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, RequestIDKey, requestID)
}

// GetRequestIDFromContext extracts request ID from context
func GetRequestIDFromContext(ctx context.Context) string {
	if requestID, ok := ctx.Value(RequestIDKey).(string); ok {
		return requestID
	}
	return ""
}

// shouldLog checks if the message should be logged based on level
func (l *Logger) shouldLog(level LogLevel) bool {
	levels := map[LogLevel]int{
		DEBUG: 0,
		INFO:  1,
		WARN:  2,
		ERROR: 3,
	}
	return levels[level] >= levels[l.level]
}

// log is the internal logging method
func (l *Logger) log(ctx context.Context, level LogLevel, message string, fields map[string]interface{}, err error) {
	if !l.shouldLog(level) {
		return
	}

	entry := LogEntry{
		Timestamp: time.Now().Format("2006-01-02 15:04:05"),
		Level:     level,
		Service:   l.service,
		Logger:    l.name,
		Message:   message,
		Fields:    fields,
	}

	// Add request ID if available
	if ctx != nil {
		if requestID := GetRequestIDFromContext(ctx); requestID != "" {
			entry.RequestID = requestID
		}
	}

	// Add error information
	if err != nil {
		entry.Error = err.Error()
		if level == ERROR {
			entry.Stack = getStackTrace()
		}
	}

	// Output the log
	fmt.Println(l.formatter.Format(entry))
}

// getStackTrace returns the current stack trace
func getStackTrace() string {
	buf := make([]byte, 1024)
	for {
		n := runtime.Stack(buf, false)
		if n < len(buf) {
			return string(buf[:n])
		}
		buf = make([]byte, 2*len(buf))
	}
}

// Public logging methods
func (l *Logger) Debug(message string) {
	l.log(nil, DEBUG, message, nil, nil)
}

func (l *Logger) DebugCtx(ctx context.Context, message string) {
	l.log(ctx, DEBUG, message, nil, nil)
}

func (l *Logger) DebugWithFields(message string, fields map[string]interface{}) {
	l.log(nil, DEBUG, message, fields, nil)
}

func (l *Logger) DebugWithFieldsCtx(ctx context.Context, message string, fields map[string]interface{}) {
	l.log(ctx, DEBUG, message, fields, nil)
}

func (l *Logger) Info(message string) {
	l.log(nil, INFO, message, nil, nil)
}

func (l *Logger) InfoCtx(ctx context.Context, message string) {
	l.log(ctx, INFO, message, nil, nil)
}

func (l *Logger) InfoWithFields(message string, fields map[string]interface{}) {
	l.log(nil, INFO, message, fields, nil)
}

func (l *Logger) InfoWithFieldsCtx(ctx context.Context, message string, fields map[string]interface{}) {
	l.log(ctx, INFO, message, fields, nil)
}

func (l *Logger) Warn(message string) {
	l.log(nil, WARN, message, nil, nil)
}

func (l *Logger) WarnCtx(ctx context.Context, message string) {
	l.log(ctx, WARN, message, nil, nil)
}

func (l *Logger) WarnWithFields(message string, fields map[string]interface{}) {
	l.log(nil, WARN, message, fields, nil)
}

func (l *Logger) WarnWithFieldsCtx(ctx context.Context, message string, fields map[string]interface{}) {
	l.log(ctx, WARN, message, fields, nil)
}

func (l *Logger) Error(message string, err error) {
	l.log(nil, ERROR, message, nil, err)
}

func (l *Logger) ErrorCtx(ctx context.Context, message string, err error) {
	l.log(ctx, ERROR, message, nil, err)
}

func (l *Logger) ErrorWithFields(message string, fields map[string]interface{}, err error) {
	l.log(nil, ERROR, message, fields, err)
}

func (l *Logger) ErrorWithFieldsCtx(ctx context.Context, message string, fields map[string]interface{}, err error) {
	l.log(ctx, ERROR, message, fields, err)
}

// Performance monitoring
type PerformanceLogger struct {
	logger    *Logger
	operation string
	startTime time.Time
	threshold time.Duration
}

// StartOperation starts performance monitoring for an operation
func (l *Logger) StartOperation(operation string, thresholdMs int) *PerformanceLogger {
	return &PerformanceLogger{
		logger:    l,
		operation: operation,
		startTime: time.Now(),
		threshold: time.Duration(thresholdMs) * time.Millisecond,
	}
}

// Complete logs the completion of an operation
func (p *PerformanceLogger) Complete(ctx context.Context) {
	duration := time.Since(p.startTime)
	if duration > p.threshold {
		p.logger.InfoWithFieldsCtx(ctx, fmt.Sprintf("Operation '%s' completed", p.operation), map[string]interface{}{
			"operation":   p.operation,
			"duration_ms": duration.Milliseconds(),
			"status":      "success",
		})
	}
}

// CompleteWithError logs the completion of an operation with an error
func (p *PerformanceLogger) CompleteWithError(ctx context.Context, err error) {
	duration := time.Since(p.startTime)
	p.logger.ErrorWithFieldsCtx(ctx, fmt.Sprintf("Operation '%s' failed", p.operation), map[string]interface{}{
		"operation":   p.operation,
		"duration_ms": duration.Milliseconds(),
		"status":      "error",
	}, err)
}

// Global convenience functions
func Debug(message string) {
	if defaultLogger == nil {
		Initialize("gateway", INFO, false)
	}
	defaultLogger.Debug(message)
}

func Info(message string) {
	if defaultLogger == nil {
		Initialize("gateway", INFO, false)
	}
	defaultLogger.Info(message)
}

func Warn(message string) {
	if defaultLogger == nil {
		Initialize("gateway", INFO, false)
	}
	defaultLogger.Warn(message)
}

func Error(message string, err error) {
	if defaultLogger == nil {
		Initialize("gateway", INFO, false)
	}
	defaultLogger.Error(message, err)
}

// Redirect standard library log to our logger
func RedirectStandardLog() {
	log.SetFlags(0)
	log.SetOutput(&logWriter{})
}

type logWriter struct{}

func (w *logWriter) Write(p []byte) (n int, err error) {
	message := string(p)
	if defaultLogger == nil {
		Initialize("gateway", INFO, false)
	}
	defaultLogger.Info(message)
	return len(p), nil
}

// Configuration from environment
func InitFromEnv() {
	service := os.Getenv("SERVICE_NAME")
	if service == "" {
		service = "gateway"
	}

	levelStr := os.Getenv("LOG_LEVEL")
	level := INFO
	switch levelStr {
	case "DEBUG":
		level = DEBUG
	case "WARN":
		level = WARN
	case "ERROR":
		level = ERROR
	}

	jsonFormat := false
	if formatStr := os.Getenv("LOG_FORMAT"); formatStr == "json" {
		jsonFormat = true
	}

	Initialize(service, level, jsonFormat)
	RedirectStandardLog()
}
