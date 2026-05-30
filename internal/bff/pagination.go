package bff

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
)

const (
	defaultPageLimit = 100
	maxPageLimit     = 500
)

type pageQuery struct {
	limit  int
	offset int
	query  string
}

func pageQueryFromRequest(r *http.Request) (pageQuery, error) {
	values := r.URL.Query()
	p := pageQuery{
		limit: defaultPageLimit,
		query: strings.ToLower(strings.TrimSpace(values.Get("q"))),
	}
	if raw := strings.TrimSpace(values.Get("limit")); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit <= 0 {
			return pageQuery{}, errors.New("limit must be a positive integer")
		}
		if limit > maxPageLimit {
			limit = maxPageLimit
		}
		p.limit = limit
	}
	if raw := strings.TrimSpace(values.Get("offset")); raw != "" {
		offset, err := strconv.Atoi(raw)
		if err != nil || offset < 0 {
			return pageQuery{}, errors.New("offset must be a non-negative integer")
		}
		p.offset = offset
	}
	return p, nil
}

func paginateRange(length, offset, limit int) (int, int) {
	if offset >= length {
		return length, length
	}
	end := offset + limit
	if end > length {
		end = length
	}
	return offset, end
}
