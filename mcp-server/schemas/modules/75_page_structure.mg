# BrowserNERD schema module: page structure observed by browser-observe
# Loaded through ../browser.mg in lexical order.

# =============================================================================
# VECTOR 17: DEAD CONTROLS (HYDRATION)
# =============================================================================
# A control inside a React root that carries no React fiber or props was never
# rendered by React, so no handler is attached to it: it looks clickable and does
# nothing. Pushed by get-interactive-elements (and so by browser-observe) only on
# pages that have a React root; links are never reported, since they navigate
# without React. browser-observe and browser-reason drop dead controls from their
# action recommendations.

Decl dead_control(SessionId, Ref, Label).

# =============================================================================
# VECTOR 18: WIDGET AND NON-SEMANTIC CONTROL CANDIDATES
# =============================================================================
# get-interactive-elements types ARIA widgets by their role (tab, combobox,
# menuitem, option) and reports elements a page made clickable without a role
# (onclick, React onClick, cursor:pointer) as "clickable". These rules make them
# action candidates alongside the buttons, inputs and links of module 80.

action_candidate(SessionId, Ref, Label, "click", 64, "tab") :-
    interactive(SessionId, Ref, "tab", Label, "click").

action_candidate(SessionId, Ref, Label, "click", 62, "open_combobox") :-
    interactive(SessionId, Ref, "combobox", Label, "click"),
    element_enabled(SessionId, Ref, "true").

action_candidate(SessionId, Ref, Label, "type", 76, "enabled_combobox_input") :-
    interactive(SessionId, Ref, "combobox", Label, "type"),
    element_enabled(SessionId, Ref, "true").

action_candidate(SessionId, Ref, Label, "click", 60, "menu_item") :-
    interactive(SessionId, Ref, "menuitem", Label, "click").

action_candidate(SessionId, Ref, Label, "click", 58, "option") :-
    interactive(SessionId, Ref, "option", Label, "click").

action_candidate(SessionId, Ref, Label, "click", 56, "clickable_element") :-
    interactive(SessionId, Ref, "clickable", Label, "click").
