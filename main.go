// Copyright 2021 ghstats Project Authors. Licensed under MIT.

package main

import (
	"fmt"
	"os"
	"path"
	"runtime"
	"strings"

	"github.com/overvenus/ghstats/cmd"
	"github.com/sirupsen/logrus"
)

func main() {
	logrus.SetReportCaller(true)
	logrus.SetFormatter(&logrus.TextFormatter{
		CallerPrettyfier: func(f *runtime.Frame) (string, string) {
			s := strings.Split(f.Function, "/")
			funcName := s[len(s)-1]
			return funcName, fmt.Sprintf("%s:%d", path.Base(f.File), f.Line)
		},
	})
	file, err := os.OpenFile("/tmp/ghstats.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o666)
	if err == nil {
		logrus.SetOutput(file)
	} else {
		logrus.Info("Failed to log to file, using default stderr")
	}

	cmd.Execute()
}
