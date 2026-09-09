cmake_minimum_required(VERSION 3.16)

if(CMAKE_SOURCE_DIR STREQUAL CMAKE_CURRENT_SOURCE_DIR)
    project(abstraction_storage LANGUAGES CXX VERSION 0.1.0)
    set(ABSTRACTION_STORAGE_STANDALONE ON)
else()
    set(ABSTRACTION_STORAGE_STANDALONE OFF)
endif()

include(GNUInstallDirs)

option(ABSTRACTION_BUILD_TESTS "Build the abstraction's own tests"
    ${ABSTRACTION_STORAGE_STANDALONE})

add_library(abstraction_storage STATIC src/storage.cpp)
target_include_directories(abstraction_storage PUBLIC
    $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/include>
    $<INSTALL_INTERFACE:${CMAKE_INSTALL_INCLUDEDIR}>
)
target_compile_features(abstraction_storage PUBLIC cxx_std_17)
set_target_properties(abstraction_storage PROPERTIES EXPORT_NAME storage)
add_library(abstraction::storage ALIAS abstraction_storage)

if(ABSTRACTION_BUILD_TESTS)
    enable_testing()
    add_executable(test_storage test/test_storage.cpp)
    target_link_libraries(test_storage PRIVATE abstraction_storage)
    add_test(NAME Storage COMMAND test_storage)
endif()

if(ABSTRACTION_STORAGE_STANDALONE)
    include(CMakePackageConfigHelpers)
    set(ABSTRACTION_STORAGE_CMAKEDIR ${CMAKE_INSTALL_LIBDIR}/cmake/abstraction_storage)

    install(TARGETS abstraction_storage EXPORT abstraction_storageTargets
        ARCHIVE DESTINATION ${CMAKE_INSTALL_LIBDIR}
        LIBRARY DESTINATION ${CMAKE_INSTALL_LIBDIR}
        RUNTIME DESTINATION ${CMAKE_INSTALL_BINDIR})
    install(DIRECTORY include/ DESTINATION ${CMAKE_INSTALL_INCLUDEDIR})
    install(EXPORT abstraction_storageTargets
        FILE abstraction_storageTargets.cmake
        NAMESPACE abstraction::
        DESTINATION ${ABSTRACTION_STORAGE_CMAKEDIR})

    configure_package_config_file(cmake/abstraction_storageConfig.cmake.in
        ${CMAKE_CURRENT_BINARY_DIR}/abstraction_storageConfig.cmake
        INSTALL_DESTINATION ${ABSTRACTION_STORAGE_CMAKEDIR})
    write_basic_package_version_file(
        ${CMAKE_CURRENT_BINARY_DIR}/abstraction_storageConfigVersion.cmake
        COMPATIBILITY SameMinorVersion)
    install(FILES
        ${CMAKE_CURRENT_BINARY_DIR}/abstraction_storageConfig.cmake
        ${CMAKE_CURRENT_BINARY_DIR}/abstraction_storageConfigVersion.cmake
        DESTINATION ${ABSTRACTION_STORAGE_CMAKEDIR})
else()
    set_property(GLOBAL APPEND PROPERTY ABSTRACTION_SOURCE_TARGETS abstraction_storage)
    set_property(GLOBAL APPEND PROPERTY ABSTRACTION_SOURCE_INCLUDES
        ${CMAKE_CURRENT_SOURCE_DIR}/include/)
endif()
