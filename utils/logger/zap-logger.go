package logger

import (
	"fmt"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
	"log"
	"os"
	"strings"
	"time"
)

var sugaredLogger *zap.SugaredLogger
var dLogger Logger

type zapLogger struct {
	sl *zap.SugaredLogger
}

func (zl zapLogger) Printf(format string, args ...interface{}) {
	zl.sl.Infof(format, args...)
}

func (zl zapLogger) Debug(args ...interface{}) {
	zl.sl.Debug(args...)
}

func (zl zapLogger) Debugf(format string, args ...interface{}) {
	zl.sl.Debugf(format, args...)
}

func (zl zapLogger) Info(args ...interface{}) {
	zl.sl.Info(args...)
}

func (zl zapLogger) Infof(format string, args ...interface{}) {
	zl.sl.Infof(format, args...)
}

func (zl zapLogger) Warn(args ...interface{}) {
	zl.sl.Warn(args...)
}

func (zl zapLogger) Warnf(format string, args ...interface{}) {
	zl.sl.Warnf(format, args...)
}

func (zl zapLogger) Fatal(args ...interface{}) {
	zl.sl.Fatal(args...)
}

func (zl zapLogger) Fatalf(format string, args ...interface{}) {
	zl.sl.Fatalf(format, args...)
}

func (zl zapLogger) Panic(args ...interface{}) {
	zl.sl.Panic(args...)
}

func (zl zapLogger) Panicf(format string, args ...interface{}) {
	zl.sl.Panicf(format, args...)
}

func (zl zapLogger) Sync() error {
	return zl.sl.Sync()
}
func (zl zapLogger) Error(args ...interface{}) {
	zl.sl.Error(args...)
}

func (zl zapLogger) Errorf(format string, args ...interface{}) {
	zl.sl.Errorf(format, args...)
}

func init() {
	dLogger = &defaultLogger{}
}
func InitTo(toConsole bool, toFile bool, level string, projectName string) {
	var logTo []string
	if toConsole {
		logTo = append(logTo, "console")
	}
	if toFile {
		logTo = append(logTo, "file")
	}

	logFilePath := "N/A"
	var cores []zapcore.Core
	var lvl zapcore.Level
	err := lvl.UnmarshalText([]byte(level))
	if err != nil {
		log.Println("invalid level:", level)
		return
	}
	if toConsole {
		cores = append(cores, zapcore.NewCore(getEncoder(), zapcore.Lock(os.Stdout), lvl))
	}
	if toFile {
		// file
		fileEncoder := getEncoder()
		logFilePath = fmt.Sprintf("/data/%s/logs/%s.log", projectName, level)
		writer := zapcore.AddSync(&lumberjack.Logger{
			Filename:   logFilePath,
			MaxSize:    200, // megabytes
			MaxBackups: 3,
			MaxAge:     30, // days
		})
		cores = append(cores, zapcore.NewCore(fileEncoder, writer, lvl))
	}
	core := zapcore.NewTee(cores...)

	sugaredLogger = zap.New(core).Sugar()
	sugaredLogger.Infof("zap logger init, console: %t, file: %t, level: %s, path: %s", toConsole, toFile, level, logFilePath)
}

func Init(to, level, projectName string) {
	var toConsole, toFile bool
	logTo := strings.ToUpper(to)

	if logTo != "" && strings.Contains(logTo, "CONSOLE") {
		toConsole = true
	}
	if logTo != "" && strings.Contains(logTo, "FILE") {
		toFile = true
	}
	InitTo(toConsole, toFile, level, projectName)
}

func GetLogger() Logger {
	if sugaredLogger != nil {
		return zapLogger{sl: sugaredLogger}
	} else {
		return dLogger
	}
}
func getEncoder() zapcore.Encoder {
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.EncodeTime = func(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
		enc.AppendString(t.Format("2006-01-02 15:04:05.000"))
	}

	return zapcore.NewConsoleEncoder(encoderConfig)
}

func Debug(args ...interface{}) {
	GetLogger().Debug(args...)
}
func Debugf(msg string, args ...interface{}) {
	GetLogger().Debugf(msg, args...)
}
func Info(args ...interface{}) {
	GetLogger().Info(args...)
}
func Infof(msg string, args ...interface{}) {
	GetLogger().Infof(msg, args...)
}
func Error(args ...interface{}) {
	GetLogger().Error(args...)
}
func Errorf(msg string, args ...interface{}) {
	GetLogger().Errorf(msg, args...)
}
func Warn(args ...interface{}) {
	GetLogger().Warn(args...)
}
func Warnf(msg string, args ...interface{}) {
	GetLogger().Errorf(msg, args...)
}
func Sync() {
	GetLogger().Sync()
}
func Fatalf(msg string, args ...interface{}) {
	GetLogger().Infof(msg, args...)
}

// Deprecated: Printf
func Println(args ...interface{}) {
	Info(args...)
}

// Deprecated: Printf
func Printf(msg string, args ...interface{}) {
	Infof(msg, args...)
}
