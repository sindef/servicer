package bff

import (
	"context"
	"net/http"
	"strings"
)

const (
	rolePlatformAdmin   = "platform-admin"
	roleTenantAdmin     = "tenant-admin"
	roleTenantOperator  = "tenant-operator"
	roleServiceConsumer = "service-consumer"
	roleAuditor         = "auditor"
	roleCatalogAdmin    = "catalog-admin"
	roleClusterAdmin    = "cluster-admin"
)

type actorContextKey struct{}

type actor struct {
	Name          string
	Email         string
	UserName      string
	Provider      string
	Subject       string
	Authenticated bool
	Roles         map[string]struct{}
	TenantRoles   map[string]map[string]struct{}
	Groups        map[string]struct{}
}

func actorFromRequest(r *http.Request) actor {
	if actor, ok := actorFromContext(r.Context()); ok {
		return actor
	}
	return actorFromHeaders(r)
}

func actorFromContext(ctx context.Context) (actor, bool) {
	if ctx == nil {
		return actor{}, false
	}
	value := ctx.Value(actorContextKey{})
	if value == nil {
		return actor{}, false
	}
	actor, ok := value.(actor)
	return actor, ok
}

func actorFromHeaders(r *http.Request) actor {
	name := strings.TrimSpace(r.Header.Get("X-Servicer-User"))
	if name == "" {
		name = "anonymous"
	}
	roles := map[string]struct{}{}
	groups := map[string]struct{}{}
	for _, role := range strings.Split(r.Header.Get("X-Servicer-Roles"), ",") {
		role = strings.TrimSpace(role)
		if role != "" {
			roles[role] = struct{}{}
		}
	}
	for _, group := range strings.Split(r.Header.Get("X-Servicer-Groups"), ",") {
		group = strings.TrimSpace(group)
		if group != "" {
			groups[group] = struct{}{}
		}
	}
	return actor{Name: name, Roles: roles, TenantRoles: map[string]map[string]struct{}{}, Groups: groups, Authenticated: name != "" && name != "anonymous"}
}

func withActor(r *http.Request, actor actor) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), actorContextKey{}, actor))
}

func (a actor) hasAny(roles ...string) bool {
	for _, role := range roles {
		if _, ok := a.Roles[role]; ok {
			return true
		}
		for _, tenantRoles := range a.TenantRoles {
			if _, ok := tenantRoles[role]; ok {
				return true
			}
		}
	}
	return false
}

func (a actor) isPlatformAdmin() bool {
	return a.hasAny(rolePlatformAdmin)
}

func (a actor) hasTenantRole(tenantName string, roles ...string) bool {
	if tenantName == "" {
		return false
	}
	tenantRoles, ok := a.TenantRoles[tenantName]
	if !ok {
		return false
	}
	for _, role := range roles {
		if _, ok := tenantRoles[role]; ok {
			return true
		}
	}
	return false
}

func (a actor) hasTenantAccess(tenantName string) bool {
	if a.isPlatformAdmin() {
		return true
	}
	return len(a.TenantRoles[tenantName]) > 0
}

func requireRole(w http.ResponseWriter, r *http.Request, roles ...string) (actor, bool) {
	actor := actorFromRequest(r)
	if actor.hasAny(roles...) {
		return actor, true
	}
	writeJSON(w, http.StatusForbidden, map[string]string{"error": "insufficient Servicer role"})
	return actor, false
}

func requirePlatformRole(w http.ResponseWriter, r *http.Request, roles ...string) (actor, bool) {
	actor := actorFromRequest(r)
	for _, role := range roles {
		if _, ok := actor.Roles[role]; ok {
			return actor, true
		}
	}
	writeJSON(w, http.StatusForbidden, map[string]string{"error": "insufficient Servicer role"})
	return actor, false
}
