package server

import (
	"developa/internal/application"
	"developa/internal/config"
)

func repositoryManagers(cfg config.Config) []application.ManagerConfig {
	repositories := cfg.Repositories
	if len(repositories) == 0 && cfg.RepositoryPath != "" {
		repositories = []config.Repository{{Name: cfg.RepositoryName, Path: cfg.RepositoryPath}}
	}
	managers := make([]application.ManagerConfig, 0, len(repositories))
	for _, repository := range repositories {
		managers = append(managers, application.ManagerConfig{RepositoryName: repository.Name, RepositoryPath: repository.Path,
			PollInterval: cfg.WatchInterval, ScanTimeout: cfg.ScanTimeout})
	}
	return managers
}

func closeAnalysisWorkers(workers []*application.AnalysisWorker) {
	for _, worker := range workers {
		worker.Close()
	}
}
