//go:build !windows && !darwin

package service

import "errors"

type noopService struct{}

func newService() Service { return noopService{} }

var errUnsupported = errors.New("service install: only Windows and macOS are supported")

func (noopService) Install(string) error    { return errUnsupported }
func (noopService) Uninstall() error        { return errUnsupported }
func (noopService) Start() error            { return errUnsupported }
func (noopService) Stop() error             { return errUnsupported }
func (noopService) Status() (Status, error) { return Status{}, errUnsupported }
