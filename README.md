# Utils

A collection of small Go helpers that can be shared between projects. The
repository is organised by package so you can import only the utilities you
need.

## File
Utilities that simplify common file system operations.

- **RemoveAll(dir string)** - Recursively delete a directory while ignoring
  errors.

  ```go
  file.RemoveAll("/tmp/cache")
  ```

- **RemoveFile(path string)** - Delete a single file and log any failures.

  ```go
  file.RemoveFile("/tmp/out.log")
  ```

- **CloseFile(c io.Closer)** - Safely close a file descriptor and log errors.

  ```go
  f, _ := os.Open("data.txt")
  file.CloseFile(f)
  ```

- **ReadLines(filename string) ([]string, error)** - Read a text file into a
  slice of lines.

  ```go
  lines, err := file.ReadLines("notes.txt")
  if err != nil {
      log.Fatal(err)
  }
  ```

- **SaveFile(dir, name string, data []byte) error** - Write a `.html` file to a
  directory, creating it if necessary.

  ```go
  err := file.SaveFile("public", "index", []byte("<h1>Hello</h1>"))
  if err != nil {
      log.Fatal(err)
  }
  ```

- **ReadFile(path string) (*bytes.Reader, error)** - Load file contents into a
  `bytes.Reader`.

  ```go
  r, err := file.ReadFile("public/index.html")
  if err != nil {
      log.Fatal(err)
  }
  ```

## Math
Helpers for basic numeric calculations and probability checks.

- **Min(a, b int) int** and **Max(a, b int) int** - Return the smaller or larger
  of two integers.

  ```go
  m := math.Min(3, 5) // 3
  M := math.Max(3, 5) // 5
  _ = m
  _ = M
  ```

- **FormatNumber(f *float64) string** - Convert a floating number to a
  human-friendly string without trailing zeros.

  ```go
  v := pointers.FromFloat(12.3400)
  s := math.FormatNumber(v) // "12.34"
  _ = s
  ```

- **ChanceOf(p float64) bool** - Return `true` with the given probability using
  cryptographic randomness.

  ```go
  if math.ChanceOf(0.1) {
      fmt.Println("10% chance hit")
  }
  ```

## Text
String normalisation helpers.

- **Normalize(s string) string** - Trim whitespace from each line and remove
  empty lines.

  ```go
  clean := text.Normalize(" Line 1 \n\n  Line 2 ")
  _ = clean
  ```

- **SanitizeToCamelCase(s string) string** - Create a camelCase identifier
  suitable for HTML IDs.

  ```go
  id := text.SanitizeToCamelCase("Example Title") // "exampleTitle"
  ```

## System
Helpers for interacting with environment variables.

- **GetEnvOrFail(name string) string** - Retrieve a required environment
  variable or exit the program.

  ```go
  token := system.GetEnvOrFail("API_TOKEN")
  _ = token
  ```

- **ExpandEnvVar(s string) (string, error)** - Expand `$VAR` style references and
  trim the result.

  ```go
  path, _ := system.ExpandEnvVar("$HOME/tmp")
  ```

## Pointers
Convenience functions for obtaining pointers to primitive values.

- **FromFloat(f float64) \*float64** - Return a pointer to the provided float.

  ```go
  ptr := pointers.FromFloat(3.14)
  _ = ptr
  ```

Unexported helpers for strings, integers and booleans exist for internal tests.

## Scheduler
Retry-aware scheduling helpers.

- **Worker** - Runs a periodic scan over pending jobs, applies exponential backoff, and persists attempt results via a repository interface.

---

### **Testing**

The tool includes **table-driven tests** to ensure consistent behavior for a variety of inputs.

**Run Tests:**

```bash
go test ./test -v
```

---

### **Dependencies**

- **[Goldmark](https://github.com/yuin/goldmark)** - Markdown rendering and parsing.
- **[html-to-markdown](https://github.com/JohannesKaufmann/html-to-markdown)** - HTML-to-Markdown conversion and cleaning.
- **[net/html](https://pkg.go.dev/golang.org/x/net/html)** - HTML parsing and rendering.

---

### **Contributing**

Contributions are welcome!

1. Fork the repository.
2. Create a new branch (`feature/my-feature`).
3. Commit changes and submit a pull request.

---

### **License**

This project is licensed under the **MIT License**. See the **LICENSE** file for details.


