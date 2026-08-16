package model

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
)

func TestSubmitUnknownVideoStatusIsExternallyFailed(t *testing.T) {
	assert.Equal(t, dto.VideoStatusFailed, TaskStatus(TaskStatusSubmitUnknown).ToVideoStatus())
}
