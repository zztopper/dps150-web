# Skill: SDE Frontend Engineer Agent

Специалист по разработке React frontend.

## Область ответственности

- React компоненты
- State management
- API интеграция
- UI/UX реализация
- Формы и валидация
- Роутинг

## Контекст проекта

**Стек:**
- React 18 + TypeScript
- Vite (сборка)
- TanStack Query (data fetching)
- React Router v6
- Ant Design (UI-библиотека)
- Zod (валидация)

**Структура:**
```
frontend/
├── src/
│   ├── api/                    # API клиент, endpoints
│   │   ├── client.ts           # Axios instance
│   │   └── {{entity}}.ts      # Entity API
│   ├── components/
│   │   ├── ui/                 # Базовые UI компоненты
│   │   ├── forms/              # Формы
│   │   └── layout/             # Layout компоненты
│   ├── features/               # Feature-based модули
│   │   └── {{feature}}/
│   │       ├── {{Feature}}List.tsx
│   │       ├── {{Feature}}Form.tsx
│   │       └── use{{Feature}}s.ts
│   ├── hooks/                  # Shared hooks
│   ├── pages/                  # Route pages
│   ├── types/                  # TypeScript типы
│   └── utils/                  # Утилиты
├── index.html
├── vite.config.ts
└── tsconfig.json
```

## Паттерны кода

### API Client
```typescript
import axios from 'axios';

export const api = axios.create({
  baseURL: import.meta.env.VITE_API_URL || '/api/v1',
});

api.interceptors.request.use((config) => {
  const token = localStorage.getItem('access_token');
  if (token) config.headers.Authorization = `Bearer ${token}`;
  return config;
});
```

### React Query Hook
```typescript
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';

export function useEntities(filter?: EntityFilter) {
  return useQuery({
    queryKey: ['entities', filter],
    queryFn: () => api.get<EntitiesResponse>('/entities', { params: filter }),
    select: (data) => data.data,
  });
}

export function useCreateEntity() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (data: EntityInput) => api.post<Entity>('/entities', data),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['entities'] }),
  });
}
```

### Protected Route
```tsx
import { Navigate, useLocation } from 'react-router-dom';
import { useAuth } from '@/features/auth/useAuth';

export function ProtectedRoute({ children, roles }: Props) {
  const { user, isLoading } = useAuth();
  const location = useLocation();
  if (isLoading) return <LoadingSpinner />;
  if (!user) return <Navigate to="/login" state={{ from: location }} replace />;
  if (roles && !roles.includes(user.role)) return <Navigate to="/forbidden" replace />;
  return children;
}
```

## Команды

```bash
cd frontend
npm run dev           # Dev server
npm run build         # Production build
npm run lint          # ESLint
npm run lint:fix      # ESLint с автофиксом
npx tsc -b            # TypeScript strict проверка
npm run test          # Vitest
npm run test:coverage # С покрытием
```

## Типичные задачи

1. **Новая страница** -> page + route + feature components
2. **Новый API endpoint** -> api function + React Query hook
3. **Форма** -> zod schema + react-hook-form / Ant Design Form + mutation
4. **Защищённый роут** -> ProtectedRoute wrapper
