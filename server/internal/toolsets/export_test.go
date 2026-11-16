package toolsets

import "log/slog"

func (s *Service) SetLoggerForTest(logger *slog.Logger) { s.logger = logger }
