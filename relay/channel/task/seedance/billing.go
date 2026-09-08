package seedance

import (
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

// EstimateBilling keeps all normal video and Context IR SKUs at their fixed
// per-call ModelPrice. Midjourney Video is the only first-phase capability
// with an output-count multiplier, and validation has already bounded it to
// 1, 2, or 4 before this method is called.
func (a *TaskAdaptor) EstimateBilling(c *gin.Context, _ *relaycommon.RelayInfo) map[string]float64 {
	parsed, err := getParsedRequest(c)
	if err != nil || parsed.Midjourney == nil || parsed.Midjourney.BatchSize == nil || *parsed.Midjourney.BatchSize == 1 {
		return nil
	}
	batchSize := *parsed.Midjourney.BatchSize
	if batchSize != 2 && batchSize != 4 {
		return nil
	}
	return map[string]float64{"batch_size": float64(batchSize)}
}
