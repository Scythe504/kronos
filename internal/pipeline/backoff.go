package pipeline

import (
	"math/big"
	"time"

	"github.com/scythe504/kronos/internal/utils"
)

const (
	BaseTDuration   time.Duration = 2 * time.Second
	CappedTDuration time.Duration = 20 * time.Second
)

// returns a jitter duration
func JitterTime(retryCount int) time.Duration {
	powerOfTwo := int64(1 << retryCount)
	backoffDuration := BaseTDuration.Milliseconds() * powerOfTwo
	minDuration := min(CappedTDuration.Milliseconds(), backoffDuration)

	jitter := utils.RandInt(big.NewInt(minDuration))

	return time.Duration(jitter) * time.Millisecond
}
