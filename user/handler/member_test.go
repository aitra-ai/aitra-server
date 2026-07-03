package handler

import (
	"errors"
	"testing"

	"github.com/gin-gonic/gin"
	mockcomp "github.com/aitra-ai/aitra-server/_mocks/github.com/aitra-ai/aitra-server/user/component"
	"github.com/aitra-ai/aitra-server/api/httpbase"
	"github.com/aitra-ai/aitra-server/builder/testutil"
	"github.com/aitra-ai/aitra-server/common/errorx"
	"github.com/aitra-ai/aitra-server/common/types"
)

type MemberTester struct {
	*testutil.GinTester
	handler *MemberHandler
	mocks   struct {
		member *mockcomp.MockMemberComponent
	}
}

func NewMemberTester(t *testing.T) *MemberTester {
	tester := &MemberTester{GinTester: testutil.NewGinTester()}
	tester.mocks.member = mockcomp.NewMockMemberComponent(t)
	tester.handler = &MemberHandler{
		c: tester.mocks.member,
	}
	return tester
}

func (t *MemberTester) WithHandleFunc(fn func(h *MemberHandler) gin.HandlerFunc) *MemberTester {
	t.Handler(fn(t.handler))
	return t
}

func Test_Membership_Delete(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Run("delete success", func(t *testing.T) {
		tester := NewMemberTester(t).WithHandleFunc(func(h *MemberHandler) gin.HandlerFunc {
			return h.Delete
		})

		req := types.RemoveMemberRequest{
			Role: "admin",
		}

		tester.mocks.member.EXPECT().Delete(tester.Gctx(), "org2", "u", "u", "admin").Return(nil)

		tester.WithUser().
			WithParam("namespace", "org2").
			WithParam("username", "u").
			WithBody(t, req).Execute()

		tester.ResponseEqSimple(t, 200, httpbase.R{
			Msg: "OK",
		})
	})
	t.Run("only 1 member", func(t *testing.T) {
		tester := NewMemberTester(t).WithHandleFunc(func(h *MemberHandler) gin.HandlerFunc {
			return h.Delete
		})

		req := types.RemoveMemberRequest{
			Role: "admin",
		}
		err := errorx.ReqParamInvalid(
			errors.New("can't remove the last member of this organization"),
			errorx.Ctx().
				Set("namespace", "org1").
				Set("detail", "can't remove the last member of this organization"),
		)
		tester.mocks.member.EXPECT().
			Delete(tester.Gctx(), "org1", "u", "u", "admin").
			Return(err)
		tester.WithUser().
			WithParam("namespace", "org1").
			WithParam("username", "u").
			WithBody(t, req).Execute()
		tester.ResponseEqSimple(t, 400, httpbase.R{
			Code:    err.(errorx.CustomError).Code(),
			Msg:     err.Error(),
			Context: err.(errorx.CustomError).Context(),
		})
	})
}
