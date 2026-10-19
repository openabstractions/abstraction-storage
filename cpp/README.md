# Storage content C++ package

Source: https://github.com/openabstractions/abstraction-storage

This C++17 package supplies generated bounded content service records and a
client that reads and writes through the Go content service. Applications keep
backend paths and storage permissions with the service owner.

```cmake
find_package(abstraction_storage_content 0.8 CONFIG REQUIRED)
target_link_libraries(your_application PRIVATE abstraction::storage_client)
```

The installed CMake targets carry the header paths and the separately installed
Identity IPC dependency. Build that dependency and this package with the
application's selected compiler and runtime. The content records and client are
interface targets; the native transport library belongs to Identity's package.

The package includes its Apache-2.0 license in this directory. Published source
selection and the application's native build remain the provenance owners.
