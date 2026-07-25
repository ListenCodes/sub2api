package main

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	accountmonitor "github.com/ListenCodes/sub2api-account-monitor"
)

type accountMonitorRuntime struct {
	handler   *accountmonitor.Handler
	collector *accountmonitor.Collector
	source    *accountmonitor.PostgresSource
	sourceDB  *sql.DB
}

func newAccountMonitorRuntime(ctx context.Context, cfg Config, extensionDB *sql.DB) (*accountMonitorRuntime, error) {
	monitorCfg := cfg.AccountMonitor
	if strings.TrimSpace(monitorCfg.SourceDatabaseURL) == "" {
		return nil, nil
	}
	if monitorCfg.Enabled && extensionDB == nil {
		return nil, errors.New("account monitor extension database is nil")
	}
	if monitorCfg.Enabled {
		if err := accountmonitor.ApplySchema(ctx, extensionDB); err != nil {
			return nil, err
		}
	}
	sourceDB, err := sql.Open("postgres", monitorCfg.SourceDatabaseURL)
	if err != nil {
		return nil, err
	}
	sourceDB.SetMaxOpenConns(4)
	sourceDB.SetMaxIdleConns(2)
	sourceDB.SetConnMaxLifetime(30 * time.Minute)
	source := accountmonitor.NewPostgresSource(sourceDB, monitorCfg.QueryTimeout, monitorCfg.BatchSize)
	runtime := &accountMonitorRuntime{source: source, sourceDB: sourceDB}
	if !monitorCfg.Enabled {
		return runtime, nil
	}
	repository := accountmonitor.NewRepository(extensionDB)
	service := accountmonitor.NewAdminService(repository, source, monitorCfg.QueryTimeout, 2*monitorCfg.PollInterval)
	runtime.handler = accountmonitor.NewHandler(service)
	runtime.collector = accountmonitor.NewCollector(source, repository, monitorCfg, nil)
	return runtime, nil
}

func (r *accountMonitorRuntime) Close() error {
	if r == nil || r.sourceDB == nil {
		return nil
	}
	return r.sourceDB.Close()
}
