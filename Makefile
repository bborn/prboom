PREFIX  ?= $(HOME)/bin
SKILLS  := $(wildcard $(HOME)/.claude $(HOME)/.claude-*)
SCRIPTS := $(notdir $(wildcard bin/*))

.PHONY: all build install uninstall clean

all: build

build:
	go build -o build/prboom .

# Symlinks, not copies: edit in the repo and the change is live immediately,
# which is the point while this is still being tuned.
install: build
	@mkdir -p $(PREFIX)
	@ln -sf $(CURDIR)/build/prboom $(PREFIX)/prboom
	@for s in $(SCRIPTS); do ln -sf $(CURDIR)/bin/$$s $(PREFIX)/$$s; done
	@echo "linked prboom $(SCRIPTS) -> $(PREFIX)"
	@n=0; for d in $(SKILLS); do \
		[ -d "$$d" ] || continue; \
		mkdir -p "$$d/skills"; \
		ln -sfn $(CURDIR)/skills/pr-walk "$$d/skills/pr-walk"; \
		n=$$((n+1)); \
	done; echo "linked pr-walk into $$n claude config dirs"

uninstall:
	@rm -f $(PREFIX)/prboom $(foreach s,$(SCRIPTS),$(PREFIX)/$(s))
	@for d in $(SKILLS); do rm -f "$$d/skills/pr-walk"; done
	@echo "unlinked"

clean:
	@rm -rf build
