package handler

import (
	"fmt"

	"github.com/aitra-ai/aitra-server/common/config"
	"github.com/aitra-ai/aitra-server/component"
)

type LicenseHandler struct {
	lc component.LicenseComponent
}

func NewLicenseHandler(config *config.Config) (*LicenseHandler, error) {
	lc, err := component.NewLicenseComponent(config)
	if err != nil {
		return nil, fmt.Errorf("fail to create license component, err: %w", err)
	}
	return &LicenseHandler{lc: lc}, nil
}
