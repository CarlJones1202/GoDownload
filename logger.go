package main

import (
	"io"
	"log"
	"os"
	"strings"
)

// Log levels
const (
	LevelDebug = iota
	LevelInfo
	LevelWarn
	LevelError
)

var (
	logger   *log.Logger
	logLevel = LevelInfo
)

func initLogger() {
	lvl := os.Getenv("LOG_LEVEL")
	switch strings.ToLower(lvl) {
	case "debug":
		logLevel = LevelDebug
	case "info":
		logLevel = LevelInfo
	case "warn", "warning":
		logLevel = LevelWarn
	case "error":
		logLevel = LevelError
	default:
		logLevel = LevelInfo
	}
	logger = log.New(os.Stderr, "", log.LstdFlags)
}

func Debugf(format string, v ...interface{}) {
	if logLevel <= LevelDebug {
		logger.Printf("[DEBUG] "+format, v...)
	}
}

func Infof(format string, v ...interface{}) {
	if logLevel <= LevelInfo {
		logger.Printf("[INFO] "+format, v...)
	}
}

func Warnf(format string, v ...interface{}) {
	if logLevel <= LevelWarn {
		logger.Printf("[WARN] "+format, v...)
	}
}

func Errorf(format string, v ...interface{}) {
	if logLevel <= LevelError {
		logger.Printf("[ERROR] "+format, v...)
	}
}

func SetLoggerOutput(w io.Writer) {
	if logger != nil {
		logger.SetOutput(w)
	}
}
