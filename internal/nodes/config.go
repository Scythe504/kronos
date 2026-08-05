package nodes

import (
	"os"

	"github.com/joho/godotenv"
	"github.com/scythe504/kronos/internal/utils"
)

// LoadAgentConfig reads agent.conf from OS-native config directory and populates environment variables.
func LoadAgentConfig() {
	agentConfPath := utils.GetAgentConfigFilePath()
	if _, err := os.Stat(agentConfPath); err == nil {
		if envMap, err := godotenv.Read(agentConfPath); err == nil {
			for k, v := range envMap {
				if os.Getenv(k) == "" {
					os.Setenv(k, v)
				}
			}
		}
	}
}
