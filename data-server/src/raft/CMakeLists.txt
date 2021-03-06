set(raft_SOURCES
    src/logger.cpp
    src/options.cpp
    src/server.cpp
    src/status.cpp
    src/impl/bulletin_board.cpp
    src/impl/logger.cpp
    src/impl/raft_fsm_candidate.cpp
    src/impl/raft_fsm.cpp
    src/impl/raft_fsm_follower.cpp
    src/impl/raft_fsm_leader.cpp
    src/impl/raft_impl.cpp
    src/impl/raft_log_unstable.cpp 
    src/impl/raft_log.cpp
    src/impl/raft_snapshot.cpp
    src/impl/raft_types.cpp 
    src/impl/replica.cpp 
    src/impl/server_impl.cpp
    src/impl/snapshot_sender.cpp
    src/impl/work_thread.cpp
    src/impl/raft.grpc.pb.cc
    src/impl/raft.pb.cc
    src/impl/storage/log_file.cpp
    src/impl/storage/log_format.cpp
    src/impl/storage/log_index.cpp
    src/impl/storage/meta_file.cpp
    src/impl/storage/storage_disk.cpp
    src/impl/storage/storage_memory.cpp
    src/impl/transport/inprocess_transport.cpp
    src/impl/transport/fast_client.cpp
    src/impl/transport/fast_connection.cpp
    src/impl/transport/fast_server.cpp
    src/impl/transport/fast_transport.cpp
    )

foreach(f IN LISTS raft_SOURCES) 
    # remove "src/" 
    string(SUBSTRING ${f} 4 -1 fname) 
    set_source_files_properties(${f} PROPERTIES 
        COMPILE_DEFINITIONS "__FNAME__=\"raft/${fname}\"") 
endforeach() 

add_library(fbase-raft STATIC ${raft_SOURCES})

set (raft_test_Deps
        fbase-raft
        fbase-common
        fbase-frame
        fbase-base
        fbase-lkqueue
        fastcommon
        ${PROTOBUF_LIBRARY}
        pthread
        )

add_subdirectory(tools)

OPTION(BUILD_RAFT_TEST "build raft tests" OFF)
MESSAGE(STATUS BUILD_RAFT_TEST=${BUILD_RAFT_TEST})
if (BUILD_RAFT_TEST)
    add_subdirectory(test)
endif()
