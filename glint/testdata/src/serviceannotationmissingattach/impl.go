package serviceannotationmissingattach

import "github.com/speakeasy-api/gram/glint/annotations"

type TestService interface {
	DoSomething()
}

type TestAuther interface {
	Authenticate()
}

type Service struct { // want `Service embeds annotations.Service but package has no func Attach\(\.\.\., \*Service\)`
	annotations.Service[TestService, TestAuther]
}

var _ TestService = (*Service)(nil)
var _ TestAuther = (*Service)(nil)

func (s *Service) DoSomething()  {}
func (s *Service) Authenticate() {}
