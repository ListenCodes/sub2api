package main

import (
	"context"
	"database/sql"
	"errors"
	"time"

	accountmonitor "github.com/ListenCodes/sub2api-account-monitor"
)

type accountMonitorRuntime struct {
	handler   *accountmonitor.Handler
	collector *accountmonitor.Collector
	sourceDB  *sql.DB
}

func newAccountMonitorRuntime(ctx context.Context, cfg Config, extensionDB *sql.DB) (*accountMonitorRuntime, error) {
	monitorCfg := cfg.AccountMonitor
	if !monitorCfg.Enabled {
		return nil, nil
	}
	if extensionDB == nil {
		return nil, errors.New("account monitor extension database is nil")
	}
	if err := accountmonitor.ApplySchema(ctx, extensionDB); err != nil {
		return nil, err
	}
	sourceDB, err := sql.Open("postgres", monitorCfg.SourceDatabaseURL)
	if err != nil {
		return nil, err
	}
	sourceDB.SetMaxOpenConns(4)
	sourceDB.SetMaxIdleConns(2)
	sourceDB.SetConnMaxLifetime(30 * time.Minute)
	source := accountmonitor.NewPostgresSource(sourceDB, monitorCfg.QueryTimeout, monitorCfg.BatchSize)
	repository := accountmonitor.NewRepository(extensionDB)
	service := accountmonitor.NewAdminService(repository, source, monitorCfg.QueryTimeout, 2*monitorCfg.PollInterval)
	return &accountMonitorRuntime{
		handler:   accountmonitor.NewHandler(service),
		collector: accountmonitor.NewCollector(source, repository, monitorCfg, nil),
		sourceDB:  sourceDB,
	}, nil
}

func (r *accountMonitorRuntime) Close() error {
	if r == nil || r.sourceDB == nil {
		return nil
	}
	return r.sourceDB.Close()
}
