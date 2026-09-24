package serviceannotationrepofield

import (
	"github.com/speakeasy-api/gram/server/internal/serviceannotationrepofield/repo"

	"github.com/speakeasy-api/gram/server/internal/annotations"
)

type TestService interface {
	DoSomething()
}

type TestAuther interface {
	Authenticate()
}

type Service struct {
	annotations.Service[TestService, TestAuther]
	repo *repo.Queries // want `field "repo" in Service has type \*github.com/speakeasy-api/gram/server/internal/serviceannotationrepofield/repo.Queries which is sqlc-generated; services should use \*pgxpool.Pool and create repo instances in methods`
}

var _ TestService = (*Service)(nil)
var _ TestAuther = (*Service)(nil)

func (s *Service) DoSomething()  {}
func (s *Service) Authenticate() {}

func Attach(service *Service) {}
