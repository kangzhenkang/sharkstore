_Pragma("once");

#include <cstdint>
#include <deque>

#include "raft_types.h"

namespace fbase {
namespace raft {
namespace impl {

class UnstableLog {
public:
    explicit UnstableLog(uint64_t offset);
    ~UnstableLog();

    UnstableLog(const UnstableLog&) = delete;
    UnstableLog& operator=(const UnstableLog&) = delete;

    uint64_t offset() const { return offset_; }

    bool maybeLastIndex(uint64_t* last_index);
    bool maybeTerm(uint64_t index, uint64_t* term);

    void stableTo(uint64_t index, uint64_t term);
    void restore(uint64_t index);

    void truncateAndAppend(const std::vector<EntryPtr>& ents);
    void slice(uint64_t lo, uint64_t hi, std::vector<EntryPtr>* ents);
    void entries(std::vector<EntryPtr>* ents);

private:
    void mustCheckOutOfBounds(uint64_t lo, uint64_t hi);

private:
    uint64_t offset_ = 0;  // 起始日志的index
    std::deque<EntryPtr> entries_;
};

} /* namespace impl */
} /* namespace raft */
} /* namespace fbase */
