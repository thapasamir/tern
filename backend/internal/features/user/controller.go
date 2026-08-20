package user

import (
	"net/http"

	"github.com/samirthapa/tern/internal/shared/httpx"
)

// Controller translates HTTP requests into service calls. It owns parsing and response
// shaping only; business rules live in Service.
type Controller struct {
	svc *Service
}

func NewController(svc *Service) *Controller { return &Controller{svc: svc} }

func (c *Controller) signup(w http.ResponseWriter, r *http.Request) error {
	var req signupRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	l, err := c.svc.Signup(r.Context(), SignupInput(req))
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, toLoginResponse(l))
	return nil
}

func (c *Controller) login(w http.ResponseWriter, r *http.Request) error {
	var req loginRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	l, err := c.svc.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, toLoginResponse(l))
	return nil
}

func (c *Controller) logout(w http.ResponseWriter, r *http.Request) error {
	p, err := requirePrincipal(r)
	if err != nil {
		return err
	}
	if err := c.svc.Logout(r.Context(), p); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (c *Controller) me(w http.ResponseWriter, r *http.Request) error {
	p, err := requirePrincipal(r)
	if err != nil {
		return err
	}
	a, err := c.svc.Me(r.Context(), p)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusOK, toAccountResponse(a))
	return nil
}
