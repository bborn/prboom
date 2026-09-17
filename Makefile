# Same place install.sh uses. Agents started by a daemon (TaskYou) get a
# non-interactive PATH, which has ~/.local/bin but not a ~/bin added in .zshrc.
PREFIX  ?= $(HOME)/.local/bin
SKILLS  := $(wildcard $(HOME)/.claude $(HOME)/.claude-*)
SCRIPTS := $(notdir $(wildcard bin/*))
SKILL_NAMES := $(notdir $(wildcard skills/*))

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
		for k in $(SKILL_NAMES); do ln -sfn $(CURDIR)/skills/$$k "$$d/skills/$$k"; done; \
		n=$$((n+1)); \
	done; echo "linked $(SKILL_NAMES) into $$n claude config dirs"

uninstall:
	@rm -f $(PREFIX)/prboom $(foreach s,$(SCRIPTS),$(PREFIX)/$(s))
	@for d in $(SKILLS); do for k in $(SKILL_NAMES); do rm -f "$$d/skills/$$k"; done; done; true
	@echo "unlinked"

clean:
	@rm -rf build
