package rpc

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"encoding/json"
	"io"

	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"golang.org/x/oauth2"
	"github.com/aitra-ai/aitra-server/common/errorx"
	"github.com/aitra-ai/aitra-server/common/utils/common"
)

type casdoorClientImpl struct {
	casClient *casdoorsdk.Client
}

var (
	_ SSOInterface = (*casdoorClientImpl)(nil)
)

func NewCasdoorClient(c *casdoorsdk.AuthConfig) SSOInterface {
	client := casdoorsdk.NewClientWithConf(c)
	return &casdoorClientImpl{
		casClient: client,
	}
}

func (c *casdoorClientImpl) UpdateUserInfo(ctx context.Context, userInfo *SSOUpdateUserInfo) error {
	casu, err := c.casClient.GetUserByUserId(userInfo.UUID)
	if err != nil {
		slog.Error("GetUserByUserId failed from casdoor", "err", err, "uuid", userInfo.UUID)
		return errorx.RemoteSvcFail(err,
			errorx.Ctx().Set("service", "casdoor").
				Set("uuid", userInfo.UUID),
		)
	}

	if casu == nil {
		return fmt.Errorf("user not found in casdoor by uuid:%s", userInfo.UUID)
	}

	if userInfo.Email != "" {
		casu.Email = userInfo.Email
	}
	if userInfo.Phone != "" {
		casu.Phone = userInfo.Phone
	}

	if userInfo.PhoneArea != "" {
		if !strings.HasPrefix(userInfo.PhoneArea, "+") {
			userInfo.PhoneArea = "+" + userInfo.PhoneArea
		}
		countryCode, err := common.GetCountryCodeByPhoneArea(casu.Phone, userInfo.PhoneArea)
		if err != nil {
			slog.Error("failed to get country code by phone area", "phone area", userInfo.PhoneArea, "error", err)
			return fmt.Errorf("failed to get country code by phone area:%s", userInfo.PhoneArea)
		}
		casu.CountryCode = countryCode
	}

	// casdoor update user api don't allow empty display name, so we set it
	if casu.DisplayName == "" {
		casu.DisplayName = casu.Name
	}

	_, err = c.casClient.UpdateUserByUserId(casu.Owner, casu.Id, casu)
	if err != nil {
		slog.Error("UpdateUserById failed from casdoor", "err", err, "id", casu.Id, "userInfo", userInfo)
		return errorx.RemoteSvcFail(err,
			errorx.Ctx().Set("service", "casdoor").
				Set("uuid", userInfo.UUID).
				Set("id", casu.Id).
				Set("owner", casu.Owner),
		)
	}

	return nil
}

func (c *casdoorClientImpl) GetUserInfo(ctx context.Context, accessToken string) (*SSOUserInfo, error) {
	claims, err := c.casClient.ParseJwtToken(accessToken)
	if err != nil {
		slog.Error("ParseJwtToken failed from casdoor", "err", err, "accessToken", accessToken)
		return nil, errorx.RemoteSvcFail(err,
			errorx.Ctx().Set("service", "casdoor").
				Set("accessToken", accessToken),
		)
	}

	return &SSOUserInfo{
		WeChat:         claims.WeChat,
		Name:           claims.User.Name,
		Email:          claims.User.Email,
		UUID:           claims.User.Id,
		RegProvider:    SSOTypeCasdoor,
		Gender:         claims.User.Gender,
		Phone:          claims.User.Phone,
		LastSigninTime: claims.User.LastSigninTime,
		Avatar:         claims.User.Avatar,
		Homepage:       claims.User.Homepage,
		Bio:            claims.User.Bio,
	}, nil
}

func (c *casdoorClientImpl) GetOAuthToken(ctx context.Context, code, state string) (*oauth2.Token, error) {
	token, err := c.casClient.GetOAuthToken(code, state)
	if err != nil {
		slog.Error("GetOAuthToken failed from casdoor", "err", err, "code", code, "state", state)
		return nil, errorx.RemoteSvcFail(err,
			errorx.Ctx().Set("service", "casdoor").
				Set("code", code).Set("state", state),
		)
	}
	return token, nil
}

func (c *casdoorClientImpl) DeleteUser(ctx context.Context, uuid string) error {
	id, err := c.casClient.GetUserByUserId(uuid)
	if err != nil {
		return err
	}
	_, err = c.casClient.DeleteUser(id)
	if err != nil {
		slog.Error("DeleteUser failed from casdoor", "err", err, "uuid", uuid)
		return errorx.ErrRemoteServiceFail
	}

	return nil
}

func (c *casdoorClientImpl) IsExistByName(ctx context.Context, name string) (bool, error) {
	user, err := c.casClient.GetUser(name)
	if err != nil {
		return false, errorx.RemoteSvcFail(err,
			errorx.Ctx().Set("service", "casdoor").
				Set("name", name),
		)
	}
	return user != nil, nil
}

func (c *casdoorClientImpl) IsExistByEmail(ctx context.Context, email string) (bool, error) {
	user, err := c.casClient.GetUserByEmail(email)
	if err != nil {
		return false, errorx.RemoteSvcFail(err,
			errorx.Ctx().Set("service", "casdoor").
				Set("email", email),
		)
	}
	return user != nil, nil
}

func (c *casdoorClientImpl) IsExistByPhone(ctx context.Context, phone string) (bool, error) {
	user, err := c.casClient.GetUserByPhone(phone)
	if err != nil {
		return false, errorx.RemoteSvcFail(err,
			errorx.Ctx().Set("service", "casdoor").
				Set("phone", phone),
		)
	}
	return user != nil, nil
}

// GetTokenByPassword authenticates with Casdoor using the OAuth2 password grant.
func (c *casdoorClientImpl) GetTokenByPassword(ctx context.Context, username, password string) (string, error) {
	cfg := c.casClient.AuthConfig
	tokenURL := fmt.Sprintf("%s/api/login/oauth/access_token", cfg.Endpoint)

	form := url.Values{}
	form.Set("grant_type", "password")
	form.Set("client_id", cfg.ClientId)
	form.Set("client_secret", cfg.ClientSecret)
	form.Set("username", username)
	form.Set("password", password)
	form.Set("scope", "read")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to call Casdoor: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var result struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to parse Casdoor response: %w", err)
	}
	if result.Error != "" {
		return "", fmt.Errorf("casdoor: %s - %s", result.Error, result.ErrorDesc)
	}
	if result.AccessToken == "" {
		return "", fmt.Errorf("casdoor returned empty access token")
	}
	return result.AccessToken, nil
}
