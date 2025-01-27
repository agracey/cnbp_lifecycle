// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/buildpacks/lifecycle (interfaces: Resolver)

// Package testmock is a generated GoMock package.
package testmock

import (
	reflect "reflect"
	sync "sync"

	gomock "github.com/golang/mock/gomock"

	buildpack "github.com/buildpacks/lifecycle/buildpack"
	platform "github.com/buildpacks/lifecycle/platform"
)

// MockResolver is a mock of Resolver interface.
type MockResolver struct {
	ctrl     *gomock.Controller
	recorder *MockResolverMockRecorder
}

// MockResolverMockRecorder is the mock recorder for MockResolver.
type MockResolverMockRecorder struct {
	mock *MockResolver
}

// NewMockResolver creates a new mock instance.
func NewMockResolver(ctrl *gomock.Controller) *MockResolver {
	mock := &MockResolver{ctrl: ctrl}
	mock.recorder = &MockResolverMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockResolver) EXPECT() *MockResolverMockRecorder {
	return m.recorder
}

// Resolve mocks base method.
func (m *MockResolver) Resolve(arg0 []buildpack.GroupElement, arg1 *sync.Map) ([]buildpack.GroupElement, []platform.BuildPlanEntry, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Resolve", arg0, arg1)
	ret0, _ := ret[0].([]buildpack.GroupElement)
	ret1, _ := ret[1].([]platform.BuildPlanEntry)
	ret2, _ := ret[2].(error)
	return ret0, ret1, ret2
}

// Resolve indicates an expected call of Resolve.
func (mr *MockResolverMockRecorder) Resolve(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Resolve", reflect.TypeOf((*MockResolver)(nil).Resolve), arg0, arg1)
}
