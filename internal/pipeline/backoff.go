package pipeline

import (
	"math"
	"math/big"
	"time"

	"github.com/scythe504/kronos/internal/utils"
)

const (
	BaseTDuration   time.Duration = 2 * time.Second
	CappedTDuration time.Duration = 20 * time.Second
)

// returns a jitter duration in milliseconds
func JitterTime(retryCount int) time.Duration {
	powerOfTwo := math.Pow(2, float64(retryCount))
	backoffDuration := BaseTDuration.Milliseconds() * int64(powerOfTwo)
	minDuration := min(CappedTDuration.Milliseconds(), backoffDuration)

	jitter := utils.RandInt(big.NewInt(minDuration))

	return time.Duration(jitter)
}
