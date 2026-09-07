package serviceannotationmissingauth

import "github.com/speakeasy-api/gram/server/internal/annotations"

type TestService interface {
	DoSomething()
}

type TestAuther interface {
	Authenticate()
}

type Service struct { // want `Service embeds annotations.Service\[\.\.\., github.com/speakeasy-api/gram/server/internal/serviceannotationmissingauth.TestAuther\] but package is missing: var _ github.com/speakeasy-api/gram/server/internal/serviceannotationmissingauth.TestAuther = \(\*Service\)\(nil\)`
	annotations.Service[TestService, TestAuther]
}

var _ TestService = (*Service)(nil)

func (s *Service) DoSomething()  {}
func (s *Service) Authenticate() {}

func Attach(service *Service) {}
