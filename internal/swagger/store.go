package swagger

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
)

// CachedStoreOptions задает режим reload и TTL для swagger-кэша.
type CachedStoreOptions struct {
	Reload   bool
	CacheTTL time.Duration
}

// CachedStore реализует Store с ленивой загрузкой и TTL-кэшированием.
type CachedStore struct {
	loader   Loader
	parser   Parser
	resolver Resolver

	reload bool
	ttl    time.Duration
	now    func() time.Time

	mu    sync.RWMutex
	state *cacheState
}

// cacheState хранит разобранный документ и предвычисленные индексы для быстрых запросов.
// Объект обновляется атомарно под mutex и читается как immutable snapshot.
type cacheState struct {
	document *Document

	endpoints         []ResolvedOperation
	endpointsByMethod map[string][]ResolvedOperation
	endpointByOpID    map[string]ResolvedOperation
	schemaByName      map[string]any

	expiresAt time.Time
}

// NewCachedStore создает swagger.Store с кэшированием.
func NewCachedStore(loader Loader, parser Parser, resolver Resolver, opts CachedStoreOptions) *CachedStore {
	ttl := opts.CacheTTL
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &CachedStore{
		loader:   loader,
		parser:   parser,
		resolver: resolver,
		reload:   opts.Reload,
		ttl:      ttl,
		now:      time.Now,
	}
}

// ListEndpoints возвращает все резолвленные endpoint-операции.
func (s *CachedStore) ListEndpoints(ctx context.Context) ([]ResolvedOperation, error) {
	st, err := s.snapshot(ctx)
	if err != nil {
		return nil, err
	}
	return copyOperations(st.endpoints), nil
}

// ListEndpointsByMethod возвращает endpoint-операции, отфильтрованные по HTTP-методу.
func (s *CachedStore) ListEndpointsByMethod(ctx context.Context, method string) ([]ResolvedOperation, error) {
	st, err := s.snapshot(ctx)
	if err != nil {
		return nil, err
	}
	key := strings.ToUpper(strings.TrimSpace(method))
	return copyOperations(st.endpointsByMethod[key]), nil
}

// GetEndpointByOperationID возвращает endpoint по operationId.
func (s *CachedStore) GetEndpointByOperationID(ctx context.Context, opID string) (ResolvedOperation, error) {
	st, err := s.snapshot(ctx)
	if err != nil {
		return ResolvedOperation{}, err
	}
	entry, ok := st.endpointByOpID[strings.TrimSpace(opID)]
	if !ok {
		return ResolvedOperation{}, ErrNotFound
	}
	return copyOperation(entry), nil
}

// GetSchemaByName возвращает схему компонента по имени с раскрытыми ссылками.
func (s *CachedStore) GetSchemaByName(ctx context.Context, name string) (any, error) {
	st, err := s.snapshot(ctx)
	if err != nil {
		return nil, err
	}
	value, ok := st.schemaByName[strings.TrimSpace(name)]
	if !ok {
		return nil, ErrNotFound
	}
	return cloneAny(value), nil
}

// Lookup возвращает произвольный объект swagger-документа по JSON pointer/path-like пути.
func (s *CachedStore) Lookup(ctx context.Context, pointer string) (any, error) {
	st, err := s.snapshot(ctx)
	if err != nil {
		return nil, err
	}

	normalized, err := normalizePointer(pointer)
	if err != nil {
		return nil, err
	}
	value, err := lookupPointer(st.document.Raw, normalized)
	if err != nil {
		return nil, err
	}
	resolved := resolveRefsInAny(cloneAny(value), st.document.Raw, make(map[string]struct{}))
	return resolved, nil
}

// snapshot возвращает актуальный снимок кэша.
// При reload=false используется TTL; при ошибке обновления возвращается предыдущий snapshot, если он есть.
func (s *CachedStore) snapshot(ctx context.Context) (*cacheState, error) {
	now := s.now()
	s.mu.RLock()
	st := s.state
	reload := s.reload
	s.mu.RUnlock()

	if !reload && st != nil && now.Before(st.expiresAt) {
		return st, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now = s.now()
	st = s.state
	if !s.reload && st != nil && now.Before(st.expiresAt) {
		return st, nil
	}

	fresh, err := s.buildState(ctx)
	if err != nil {
		if st != nil {
			return st, nil
		}
		return nil, err
	}
	s.state = fresh
	return fresh, nil
}

// buildState полностью пересобирает индексы из текущего swagger-источника.
// Если resolver не того типа, schema-index не может быть построен корректно и возвращается ошибка.
func (s *CachedStore) buildState(ctx context.Context) (*cacheState, error) {
	if s.loader == nil || s.parser == nil || s.resolver == nil {
		return nil, fmt.Errorf("swagger store is not fully configured")
	}

	raw, err := s.loader.Load(ctx)
	if err != nil {
		return nil, err
	}
	doc, err := s.parser.Parse(ctx, raw)
	if err != nil {
		return nil, err
	}
	if doc == nil || doc.Spec == nil {
		return nil, fmt.Errorf("empty parsed swagger document")
	}

	operations := collectOperations(doc)
	resolved := make([]ResolvedOperation, 0, len(operations))
	byMethod := make(map[string][]ResolvedOperation)
	byID := make(map[string]ResolvedOperation)

	for _, op := range operations {
		entry, err := s.resolver.ResolveEndpoint(ctx, op)
		if err != nil {
			return nil, fmt.Errorf("resolve operation %s %s: %w", op.Method, op.Path, err)
		}
		resolved = append(resolved, entry)
		methodKey := strings.ToUpper(strings.TrimSpace(entry.Method))
		byMethod[methodKey] = append(byMethod[methodKey], entry)
		byID[entry.OperationID] = entry
	}

	sortOperations(resolved)
	for key, list := range byMethod {
		sortOperations(list)
		byMethod[key] = list
	}

	schemas := make(map[string]any)
	if doc.Spec.Components != nil {
		resolver, ok := s.resolver.(*OpenAPIResolver)
		if !ok {
			return nil, fmt.Errorf("resolver must be *OpenAPIResolver")
		}
		for name, schemaRef := range doc.Spec.Components.Schemas {
			schemas[name] = resolver.resolveSchemaRef(schemaRef, make(map[*openapi3.Schema]struct{}))
		}
	}

	return &cacheState{
		document:          doc,
		endpoints:         resolved,
		endpointsByMethod: byMethod,
		endpointByOpID:    byID,
		schemaByName:      schemas,
		expiresAt:         s.now().Add(s.ttl),
	}, nil
}

// collectOperations извлекает операции из paths/methods в детерминированном порядке.
// Для операций без operationId генерируется стабильный synthetic ID.
func collectOperations(doc *Document) []Operation {
	if doc == nil || doc.Spec == nil || doc.Spec.Paths == nil {
		return nil
	}

	pathMap := doc.Spec.Paths.Map()
	paths := make([]string, 0, len(pathMap))
	for path := range pathMap {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	out := make([]Operation, 0)
	for _, path := range paths {
		pathItem := pathMap[path]
		if pathItem == nil {
			continue
		}
		ops := pathItem.Operations()
		methods := make([]string, 0, len(ops))
		for method := range ops {
			methods = append(methods, strings.ToUpper(method))
		}
		sort.Slice(methods, func(i, j int) bool {
			return methodSortKey(methods[i]) < methodSortKey(methods[j])
		})

		for _, method := range methods {
			op := pathItem.GetOperation(strings.ToUpper(method))
			if op == nil {
				op = pathItem.GetOperation(strings.ToLower(method))
			}
			if op == nil {
				continue
			}
			opID := op.OperationID
			if strings.TrimSpace(opID) == "" {
				opID = syntheticOperationID(method, path)
			}
			out = append(out, Operation{
				Method:      strings.ToUpper(method),
				Path:        path,
				OperationID: opID,
				PathItem:    pathItem,
				Value:       op,
				Doc:         doc,
			})
		}
	}
	return out
}

// methodSortKey выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func methodSortKey(method string) string {
	method = strings.ToUpper(strings.TrimSpace(method))
	order, ok := methodOrder[method]
	if !ok {
		return fmt.Sprintf("999_%s", method)
	}
	return fmt.Sprintf("%03d_%s", order, method)
}

// sortOperations выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func sortOperations(operations []ResolvedOperation) {
	sort.Slice(operations, func(i, j int) bool {
		left := operations[i]
		right := operations[j]
		leftMethod := methodSortKey(left.Method)
		rightMethod := methodSortKey(right.Method)
		if leftMethod != rightMethod {
			return leftMethod < rightMethod
		}
		if left.PathTemplate != right.PathTemplate {
			return left.PathTemplate < right.PathTemplate
		}
		return left.OperationID < right.OperationID
	})
}

// copyOperations возвращает независимую копию значения, чтобы избежать побочных мутаций.
func copyOperations(input []ResolvedOperation) []ResolvedOperation {
	if len(input) == 0 {
		return nil
	}
	out := make([]ResolvedOperation, len(input))
	for i := range input {
		out[i] = copyOperation(input[i])
	}
	return out
}

// copyOperation возвращает независимую копию значения, чтобы избежать побочных мутаций.
func copyOperation(value ResolvedOperation) ResolvedOperation {
	value.Tags = append([]string(nil), value.Tags...)
	value.Servers = append([]string(nil), value.Servers...)
	value.PathParams = append([]Param(nil), value.PathParams...)
	value.QueryParams = append([]Param(nil), value.QueryParams...)
	value.HeaderParams = append([]Param(nil), value.HeaderParams...)
	value.CookieParams = append([]Param(nil), value.CookieParams...)
	value.Request.ContentTypes = append([]string(nil), value.Request.ContentTypes...)
	value.Request.BodySchema = cloneAny(value.Request.BodySchema)
	value.Request.Examples = cloneAny(value.Request.Examples)
	value.Responses.Success = append([]Response(nil), value.Responses.Success...)
	value.Responses.Errors = append([]ErrorResponse(nil), value.Responses.Errors...)
	for i := range value.PathParams {
		value.PathParams[i].Schema = cloneAny(value.PathParams[i].Schema)
	}
	for i := range value.QueryParams {
		value.QueryParams[i].Schema = cloneAny(value.QueryParams[i].Schema)
	}
	for i := range value.HeaderParams {
		value.HeaderParams[i].Schema = cloneAny(value.HeaderParams[i].Schema)
	}
	for i := range value.CookieParams {
		value.CookieParams[i].Schema = cloneAny(value.CookieParams[i].Schema)
	}
	for i := range value.Responses.Success {
		value.Responses.Success[i].ContentTypes = append([]string(nil), value.Responses.Success[i].ContentTypes...)
		value.Responses.Success[i].BodySchema = cloneAny(value.Responses.Success[i].BodySchema)
	}
	for i := range value.Responses.Errors {
		value.Responses.Errors[i].ContentTypes = append([]string(nil), value.Responses.Errors[i].ContentTypes...)
		value.Responses.Errors[i].BodySchema = cloneAny(value.Responses.Errors[i].BodySchema)
	}
	value.Security = cloneAny(value.Security)
	return value
}

// normalizePointer нормализует входные данные к канонической форме, используемой в модуле.
func normalizePointer(pointer string) (string, error) {
	pointer = strings.TrimSpace(pointer)
	if pointer == "" || pointer == "/" {
		return "", nil
	}
	decoded, err := url.QueryUnescape(pointer)
	if err != nil {
		decoded = pointer
	}
	decoded = strings.TrimSpace(decoded)
	decoded = strings.TrimPrefix(decoded, "#")
	if decoded == "" || decoded == "/" {
		return "", nil
	}
	if !strings.HasPrefix(decoded, "/") {
		decoded = "/" + decoded
	}
	return decoded, nil
}

// lookupPointer ищет значение по указателю/ключу с обработкой пограничных случаев.
func lookupPointer(root any, pointer string) (any, error) {
	if pointer == "" {
		return root, nil
	}
	parts := strings.Split(strings.TrimPrefix(pointer, "/"), "/")
	current := root
	for _, part := range parts {
		token := decodePointerToken(part)
		switch node := current.(type) {
		case map[string]any:
			next, ok := node[token]
			if !ok {
				return nil, ErrNotFound
			}
			current = next
		case []any:
			idx, err := strconv.Atoi(token)
			if err != nil || idx < 0 || idx >= len(node) {
				return nil, ErrNotFound
			}
			current = node[idx]
		default:
			return nil, ErrNotFound
		}
	}
	return current, nil
}

// decodePointerToken обновляет соответствующий счетчик или метрику наблюдаемости.
func decodePointerToken(token string) string {
	token = strings.ReplaceAll(token, "~1", "/")
	token = strings.ReplaceAll(token, "~0", "~")
	return token
}

// resolveRefsInAny выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func resolveRefsInAny(value any, root any, seen map[string]struct{}) any {
	switch node := value.(type) {
	case map[string]any:
		if rawRef, ok := node["$ref"].(string); ok {
			pointer, ok := referenceToPointer(rawRef)
			if ok {
				if _, exists := seen[pointer]; exists {
					return map[string]any{"x-circularRef": rawRef}
				}
				seen[pointer] = struct{}{}
				resolved, err := lookupPointer(root, pointer)
				if err == nil {
					expanded := resolveRefsInAny(cloneAny(resolved), root, seen)
					if len(node) == 1 {
						delete(seen, pointer)
						return expanded
					}
					if base, ok := expanded.(map[string]any); ok {
						merged := cloneAny(base).(map[string]any)
						for key, val := range node {
							if key == "$ref" {
								continue
							}
							merged[key] = resolveRefsInAny(val, root, seen)
						}
						delete(seen, pointer)
						return merged
					}
				}
				delete(seen, pointer)
			}
		}
		out := make(map[string]any, len(node))
		for key, val := range node {
			out[key] = resolveRefsInAny(val, root, seen)
		}
		return out
	case []any:
		out := make([]any, len(node))
		for i := range node {
			out[i] = resolveRefsInAny(node[i], root, seen)
		}
		return out
	default:
		return node
	}
}

// referenceToPointer выполняет локальный вспомогательный шаг для снижения сложности основной функции.
func referenceToPointer(ref string) (string, bool) {
	ref = strings.TrimSpace(ref)
	if !strings.HasPrefix(ref, "#/") {
		return "", false
	}
	return strings.TrimPrefix(ref, "#"), true
}
