#include "impl/logger.h"

namespace fbase {
namespace raft {

void SetLogger(Logger* logger) {
    delete impl::g_logger;
    impl::g_logger = logger;
}

} /* namespace raft */
} /* namespace fbase */