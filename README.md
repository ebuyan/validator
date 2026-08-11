# validator

Тонкая обёртка над [`go-playground/validator/v10`](https://github.com/go-playground/validator).
Делает три вещи:

- глобальный валидатор с регистрацией кастомных правил — `Register`;
- разбор запроса (JSON / form / multipart) с последующей валидацией — `BindJSON`;
- человекочитаемые ошибки: плоская мапа `путь-до-поля → причина`, где путь собран
  по `json`/`schema`-тегам, а не по именам Go-полей.

```
go get github.com/ebuyan/validator
```

```go
import "github.com/ebuyan/validator"
```

## Инициализация

Валидатор глобальный и ленивый: до первого вызова `Register` он не готов. Зовите
`Register` один раз на старте приложения (в `main`), а в тестах — в `TestMain`
или в начале теста.

```go
func main() {
	validator.Register() // без кастомных правил
	// ...
}
```

Вызов `Validate`/`BindJSON` до `Register` не паникует, а возвращает
`Error{Msg: "validator is not registered: call Register first"}`.

`Register` идемпотентен: пересоздаёт валидатор и заново заполняет набор сообщений,
повторный вызов не накапливает правила от прошлых вызовов.

## API

| Функция | Назначение |
|---------|-----------|
| `Register(custom ...CustomValidator)` | Инициализирует глобальный валидатор и регистрирует кастомные правила. |
| `BindJSON(object any, r *http.Request) error` | Декодирует тело запроса в `object` и валидирует его. |
| `Validate(object any) error` | Валидирует **структуру** (или указатель на неё) по тегам `validate`. |
| `ValidateValue(value any) error` | Валидирует значение **любого вида** верхнего уровня: структуру, слайс, массив или мапу. |

### Validate — структуры

```go
type CreateUserRequest struct {
	Login string `json:"login" validate:"required"`
	Email string `json:"email" validate:"required,email"`
	Age   int    `json:"age"   validate:"gte=0,lte=130"`
}

req := CreateUserRequest{Login: "", Email: "not-an-email", Age: 200}

if err := validator.Validate(&req); err != nil {
	// login=required;email=email;age=lte=130
	// (порядок полей не гарантирован — Fields это мапа, см. «Ошибки»)
	fmt.Println(err)
}
```

Принимается и значение, и указатель.

### ValidateValue — слайсы, массивы, мапы и вложенность

`go-playground` (и потому `Validate`) валидирует только структуру верхнего уровня.
`ValidateValue` снимает ограничение: заворачивает значение в анонимную обёртку и сам
проставляет нужную цепочку `dive`, поэтому теги проверяются и на конечных элементах —
в т.ч. для `[][]T` и `map[K][]T`. Указатели разыменовываются; `nil` и скаляры — no-op.

```go
type Rule struct {
	Source    string   `json:"source"    validate:"required"`
	Operators []string `json:"operators" validate:"required,min=1"`
}

// Слайс структур
_ = validator.ValidateValue([]Rule{{Source: "app"}})
// 0.operators=required

// Вложенный слайс
_ = validator.ValidateValue([][]Rule{{{Operators: []string{"="}}}})
// 0.0.source=required

// Мапа структур
_ = validator.ValidateValue(map[string]Rule{"g": {Operators: []string{"="}}})
// g.source=required

// Мапа слайсов
_ = validator.ValidateValue(map[string][]Rule{"g": {{Operators: []string{"="}}}})
// g.0.source=required

// no-op — ошибки нет
_ = validator.ValidateValue(nil)
_ = validator.ValidateValue("plain string")   // не структура/коллекция
_ = validator.ValidateValue([]Rule{})         // пустая коллекция
```

Когда что брать:

- корень — структура → подойдёт и `Validate`, и `ValidateValue`;
- корень — слайс / массив / мапа → **только** `ValidateValue` (`Validate` вернёт
  общую ошибку от `go-playground`, а не пройдёт по элементам).

### BindJSON — разбор запроса + валидация

`BindJSON` сам выбирает декодер по запросу и затем валидирует результат:

- `GET` / `application/x-www-form-urlencoded` → `gorilla/schema` (теги `schema:"..."`),
  неизвестные ключи игнорируются;
- `multipart/form-data` → разбор формы (до 32 MiB);
- остальное (`POST` с JSON) → `encoding/json` (теги `json:"..."`).

```go
type CreateRequest struct {
	Name string `json:"name" schema:"name" validate:"required"`
}

func handler(w http.ResponseWriter, r *http.Request) {
	var req CreateRequest
	if err := validator.BindJSON(&req, r); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// req заполнен и провалидирован
}
```

Ошибки декодирования (битое тело, неразбираемая форма) возвращаются как есть и
**не** оборачиваются в `Error` — это другой класс ошибок, а не семантическая
валидация. Если нужна единая обработка — разбирайте их отдельно.

## Ошибки

При ошибке валидации возвращается `validator.Error`:

```go
type Error struct {
	Msg    string            // общее сообщение (валидатор не зарегистрирован и т.п.)
	Fields map[string]string // путь-до-поля → причина
}
```

`Error()` собирает `Fields` в строку `ключ=значение`, через `;`:

```
login=required;age=lte=130
```

- **Ключ** — путь до поля по `json`/`schema`-тегам (`schema` приоритетнее). Для
  коллекций в путь попадают индексы и ключи: `0.operators`, `0.0.source`, `g.0.source`.
- **Значение** — имя сработавшего тега (с параметром, если он есть: `lte=130`).
  Если для тега задан `Message` в `CustomValidator` — подставляется он.

`Fields` — мапа, поэтому **порядок полей** в строке не гарантирован. Нужен
стабильный вывод — обходите `Fields` сами с сортировкой ключей.

На одно поле приходится одна причина: `go-playground` короткозамыкает и отдаёт
первый непрошедший тег, а не все нарушенные правила поля.

Разбор по полям:

```go
var e validator.Error
if errors.As(err, &e) {
	for path, reason := range e.Fields {
		log.Printf("поле %q невалидно: %s", path, reason)
	}
}
```

## Кастомные правила

```go
import v10 "github.com/go-playground/validator/v10"

func isEven(fl v10.FieldLevel) bool {
	return fl.Field().Int()%2 == 0
}

validator.Register(
	validator.CustomValidator{Name: "even", Fn: isEven, Message: "must be even"},
)

type T struct {
	N int `json:"n" validate:"even"`
}

_ = validator.Validate(&T{N: 3}) // n=must be even
```

`Message` необязателен: если он пустой, в ошибку попадёт имя тега (`n=even`).
Сообщения привязаны к тегу и применяются ко всем полям с этим тегом.
