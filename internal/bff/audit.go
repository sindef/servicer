package bff

import (
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	actor, ok := requireRole(w, r, rolePlatformAdmin, roleTenantOperator, roleServiceConsumer)
	if !ok {
		return
	}
	filter, err := auditFilterFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	events, err := s.auditEvents(r, actor)
	if err != nil {
		writeError(w, err)
		return
	}
	events = filter.apply(events)
	writeJSON(w, http.StatusOK, events)
}

func (s *Server) auditEvents(r *http.Request, actor actor) ([]AuditEventSummary, error) {
	var actions platformv1alpha1.ActionRequestList
	if err := s.client.List(r.Context(), &actions); err != nil {
		return nil, err
	}
	var tenants platformv1alpha1.TenantList
	if err := s.client.List(r.Context(), &tenants); err != nil {
		return nil, err
	}
	var projects platformv1alpha1.ProjectList
	if err := s.client.List(r.Context(), &projects); err != nil {
		return nil, err
	}
	var instances platformv1alpha1.ServiceInstanceList
	if err := s.client.List(r.Context(), &instances); err != nil {
		return nil, err
	}
	projects.Items = visibleProjects(actor, projects.Items, tenants.Items)
	instances.Items = visibleInstances(actor, instances.Items, projects.Items, tenants.Items)
	actions.Items = visibleActions(actor, actions.Items, instances.Items)
	var kubeEvents corev1.EventList
	if err := s.client.List(r.Context(), &kubeEvents); err != nil {
		return nil, err
	}
	allowedInvolved := map[string]struct{}{}
	for _, instance := range instances.Items {
		allowedInvolved[instance.Name] = struct{}{}
		if instance.Status.Placement.Namespace != "" {
			allowedInvolved["Namespace/"+instance.Status.Placement.Namespace] = struct{}{}
		}
		if instance.Status.Runtime.ObjectRef != nil {
			allowedInvolved[instance.Status.Runtime.ObjectRef.Kind+"/"+instance.Status.Runtime.ObjectRef.Name] = struct{}{}
		}
	}
	events := make([]AuditEventSummary, 0, len(actions.Items)+len(kubeEvents.Items))
	for _, action := range actions.Items {
		events = append(events, AuditEventSummary{
			Time:     timestamp(action.CreationTimestamp),
			Type:     "ActionRequest",
			Subject:  action.Name,
			Action:   action.Spec.Action,
			Actor:    action.Spec.RequestedBy.Subject,
			Phase:    action.Status.Phase,
			Message:  action.Status.Result.Message,
			Involved: action.Spec.TargetRef.Name,
		})
	}
	for _, event := range kubeEvents.Items {
		involved := event.InvolvedObject.Kind + "/" + event.InvolvedObject.Name
		if !actor.isPlatformAdmin() {
			if _, ok := allowedInvolved[involved]; !ok {
				continue
			}
		}
		events = append(events, AuditEventSummary{
			Time:     eventTimestamp(event),
			Type:     "KubernetesEvent",
			Subject:  event.Name,
			Reason:   event.Reason,
			Message:  event.Message,
			Involved: involved,
		})
	}
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].Time != events[j].Time {
			return events[i].Time > events[j].Time
		}
		if events[i].Type != events[j].Type {
			return events[i].Type < events[j].Type
		}
		if events[i].Subject != events[j].Subject {
			return events[i].Subject < events[j].Subject
		}
		if events[i].Reason != events[j].Reason {
			return events[i].Reason < events[j].Reason
		}
		return events[i].Message < events[j].Message
	})
	if s.auditStore != nil {
		var err error
		events, err = s.auditStore.merge(r.Context(), events)
		if err != nil {
			return nil, err
		}
		sort.SliceStable(events, func(i, j int) bool {
			if events[i].Time != events[j].Time {
				return events[i].Time > events[j].Time
			}
			if events[i].Type != events[j].Type {
				return events[i].Type < events[j].Type
			}
			if events[i].Subject != events[j].Subject {
				return events[i].Subject < events[j].Subject
			}
			if events[i].Reason != events[j].Reason {
				return events[i].Reason < events[j].Reason
			}
			return events[i].Message < events[j].Message
		})
	}
	return events, nil
}

func (s *Server) eventsForTarget(r *http.Request, target string, limit int) []AuditEventSummary {
	actor := actorFromRequest(r)
	events, err := s.auditEvents(r, actor)
	if err != nil {
		return nil
	}
	filtered := make([]AuditEventSummary, 0)
	for _, event := range events {
		if event.Involved == target || strings.Contains(event.Involved, "/"+target) || event.Subject == target {
			filtered = append(filtered, event)
		}
	}
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered
}

func eventTimestamp(event corev1.Event) string {
	if !event.EventTime.IsZero() {
		return event.EventTime.Time.Format("2006-01-02T15:04:05Z07:00")
	}
	if !event.FirstTimestamp.IsZero() {
		return timestamp(event.FirstTimestamp)
	}
	return timestamp(event.CreationTimestamp)
}

type auditFilter struct {
	query     string
	actor     string
	eventType string
	resource  string
	action    string
	phase     string
	from      time.Time
	to        time.Time
	limit     int
}

func auditFilterFromRequest(r *http.Request) (auditFilter, error) {
	filter := auditFilter{
		query:     strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q"))),
		actor:     strings.ToLower(strings.TrimSpace(r.URL.Query().Get("actor"))),
		eventType: strings.ToLower(strings.TrimSpace(r.URL.Query().Get("type"))),
		resource:  strings.ToLower(strings.TrimSpace(r.URL.Query().Get("resource"))),
		action:    strings.ToLower(strings.TrimSpace(r.URL.Query().Get("action"))),
		phase:     strings.ToLower(strings.TrimSpace(r.URL.Query().Get("phase"))),
		limit:     100,
	}
	if rawLimit := strings.TrimSpace(r.URL.Query().Get("limit")); rawLimit != "" {
		limit, err := strconv.Atoi(rawLimit)
		if err != nil || limit <= 0 {
			return auditFilter{}, errors.New("limit must be a positive integer")
		}
		if limit > 500 {
			limit = 500
		}
		filter.limit = limit
	}
	if rawFrom := strings.TrimSpace(r.URL.Query().Get("from")); rawFrom != "" {
		parsed, err := time.Parse(time.RFC3339, rawFrom)
		if err != nil {
			return auditFilter{}, errors.New("from must be RFC3339")
		}
		filter.from = parsed
	}
	if rawTo := strings.TrimSpace(r.URL.Query().Get("to")); rawTo != "" {
		parsed, err := time.Parse(time.RFC3339, rawTo)
		if err != nil {
			return auditFilter{}, errors.New("to must be RFC3339")
		}
		filter.to = parsed
	}
	return filter, nil
}

func (f auditFilter) apply(events []AuditEventSummary) []AuditEventSummary {
	filtered := make([]AuditEventSummary, 0, len(events))
	for _, event := range events {
		if f.query != "" && !strings.Contains(strings.ToLower(event.Subject+" "+event.Action+" "+event.Actor+" "+event.Phase+" "+event.Reason+" "+event.Message+" "+event.Involved), f.query) {
			continue
		}
		if f.actor != "" && !strings.Contains(strings.ToLower(event.Actor), f.actor) {
			continue
		}
		if f.eventType != "" && strings.ToLower(event.Type) != f.eventType {
			continue
		}
		if f.resource != "" && !strings.Contains(strings.ToLower(event.Involved+" "+event.Subject), f.resource) {
			continue
		}
		if f.action != "" && strings.ToLower(event.Action) != f.action {
			continue
		}
		if f.phase != "" && strings.ToLower(event.Phase) != f.phase {
			continue
		}
		if !f.from.IsZero() || !f.to.IsZero() {
			eventTime, err := time.Parse(time.RFC3339, event.Time)
			if err == nil {
				if !f.from.IsZero() && eventTime.Before(f.from) {
					continue
				}
				if !f.to.IsZero() && eventTime.After(f.to) {
					continue
				}
			}
		}
		filtered = append(filtered, event)
		if f.limit > 0 && len(filtered) >= f.limit {
			break
		}
	}
	return filtered
}
