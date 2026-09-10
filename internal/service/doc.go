// Package service contains business-logic implementations.
//
// Convention: one file per feature (e.g. approval_service.go), each exposing
// an interface consumed by handlers and implemented against a repository
// interface from the repository package. Keep Fiber types (*fiber.Ctx) out
// of this layer so business logic stays transport-agnostic.
package service
