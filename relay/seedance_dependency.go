package relay

import (
	"context"

	"github.com/QuantumNous/new-api/model"
)

type seedanceDependencyResolver struct{}

func (seedanceDependencyResolver) ResolveTaskReference(ctx context.Context, userID, channelID int, localTaskID, purpose string) (string, error) {
	return model.ResolveSeedanceTaskReference(ctx, userID, channelID, localTaskID, purpose)
}

func (seedanceDependencyResolver) ResolveArtifact(ctx context.Context, userID, channelID int, localHandle, kind string) (string, error) {
	return model.ResolveSeedanceArtifact(ctx, userID, channelID, localHandle, kind)
}
