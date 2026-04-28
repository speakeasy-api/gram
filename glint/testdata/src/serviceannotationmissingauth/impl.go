package serviceannotationmissingauth

import "github.com/speakeasy-api/gram/glint/annotations"

type TestService interface {
	DoSomething()
}

type TestAuther interface {
	Authenticate()
}

type Service struct { // want `Service embeds annotations.Service\[\.\.\., serviceannotationmissingauth.TestAuther\] but package is missing: var _ serviceannotationmissingauth.TestAuther = \(\*Service\)\(nil\)`
	annotations.Service[TestService, TestAuther]
}

var _ TestService = (*Service)(nil)

func (s *Service) DoSomething()  {}
func (s *Service) Authenticate() {}

func Attach(service *Service) {}
