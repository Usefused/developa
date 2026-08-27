package application

import (
	"context"

	"developa/internal/domain"
)

const defaultFeatureSourceLimit = 20

type featureContextStore interface {
	Feature(context.Context, string, string, string) (domain.Feature, error)
	FeatureContext(context.Context, string, string, string, int) (domain.ContextPack, error)
	Flow(context.Context, string, string, domain.FlowOptions) (domain.CodeFlow, error)
}

type FeatureContexts struct {
	repositoryID string
	store        featureContextStore
}

func NewFeatureContexts(store featureContextStore, repositoryID string) *FeatureContexts {
	return &FeatureContexts{repositoryID: repositoryID, store: store}
}

func (s *FeatureContexts) FeatureContext(ctx context.Context, snapshotID, featureID string, options domain.FeatureContextOptions) (domain.FeatureContextBundle, error) {
	options, flowOptions, err := normalizeFeatureContextOptions(featureID, options)
	if err != nil {
		return domain.FeatureContextBundle{}, err
	}
	feature, err := s.store.Feature(ctx, s.repositoryID, snapshotID, featureID)
	if err != nil {
		return domain.FeatureContextBundle{}, err
	}
	source, err := s.store.FeatureContext(ctx, s.repositoryID, snapshotID, featureID, options.SourceLimit)
	if err != nil {
		return domain.FeatureContextBundle{}, err
	}
	flow, err := s.store.Flow(ctx, s.repositoryID, snapshotID, flowOptions)
	if err != nil {
		return domain.FeatureContextBundle{}, err
	}
	return featureContextBundle(s.repositoryID, snapshotID, options, feature, source, flow), nil
}

func normalizeFeatureContextOptions(featureID string, options domain.FeatureContextOptions) (domain.FeatureContextOptions, domain.FlowOptions, error) {
	if options.SourceLimit == 0 {
		options.SourceLimit = defaultFeatureSourceLimit
	}
	if options.SourceLimit < 1 || options.SourceLimit > 20 {
		return options, domain.FlowOptions{}, domain.ErrInvalidInput
	}
	flow, err := domain.NormalizeFlowOptions(domain.FlowOptions{FeatureID: featureID, Depth: options.Depth, Limit: options.FlowLimit})
	if err != nil {
		return options, flow, err
	}
	options.Depth, options.FlowLimit = flow.Depth, flow.Limit
	return options, flow, nil
}

func featureContextBundle(repositoryID, snapshotID string, options domain.FeatureContextOptions, feature domain.Feature, source domain.ContextPack, flow domain.CodeFlow) domain.FeatureContextBundle {
	limitations := []string{"Feature title and summary are inferred claims. Source records and resolved static calls are the supporting evidence."}
	if source.Truncated {
		limitations = append(limitations, "The source evidence pack is truncated; request a different source_limit only within the published bound.")
	}
	if flow.Truncated {
		limitations = append(limitations, "The resolved feature flow is truncated by its depth, node, seed, or edge bound.")
	}
	return domain.FeatureContextBundle{RepositoryID: repositoryID, SnapshotID: snapshotID, Options: options, Feature: feature, Source: source, Flow: flow, Limitations: limitations}
}
