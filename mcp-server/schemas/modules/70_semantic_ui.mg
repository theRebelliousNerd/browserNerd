# BrowserNERD schema module: semantic UI macros
# Loaded through ../browser.mg in lexical order.

# =============================================================================
# VECTOR 14: SEMANTIC UI MACROS
# =============================================================================
# High-level semantic abstractions for common UI patterns, enabling agents
# to reason about "main content", "primary actions", and "obstructions".

# --- Screen Obstruction Detection ---
Decl screen_blocked(SessionId, NodeId, Reason).

screen_blocked(SessionId, Id, "modal") :- dom_attr(SessionId, Id, "class", "modal").
screen_blocked(SessionId, Id, "modal") :-
    dom_attr(SessionId, Id, "class", Class),
    :string:contains(Class, "modal").
screen_blocked(SessionId, Id, "modal-backdrop") :- dom_attr(SessionId, Id, "class", "modal-backdrop").
screen_blocked(SessionId, Id, "modal-backdrop") :-
    dom_attr(SessionId, Id, "class", Class),
    :string:contains(Class, "modal-backdrop").
screen_blocked(SessionId, Id, "dialog") :- dom_attr(SessionId, Id, "role", "dialog").
screen_blocked(SessionId, Id, "alertdialog") :- dom_attr(SessionId, Id, "role", "alertdialog").
screen_blocked(SessionId, Id, "loading-overlay") :-
    dom_attr(SessionId, Id, "id", "loading-overlay").
screen_blocked(SessionId, Id, "spinner") :- dom_attr(SessionId, Id, "class", "loading-spinner").
screen_blocked(SessionId, Id, "spinner") :-
    dom_attr(SessionId, Id, "class", Class),
    :string:contains(Class, "spinner").

# Derived: Page interaction is blocked
Decl interaction_blocked(SessionId, Reason).
interaction_blocked(SessionId, Reason) :-
    current_url(SessionId, _),
    screen_blocked(SessionId, _, Reason).

# --- Main Content Detection ---
Decl is_main_content(SessionId, NodeId).
is_main_content(SessionId, Id) :- dom_node(SessionId, Id, "main", _, _).
is_main_content(SessionId, Id) :- dom_attr(SessionId, Id, "id", "main").
is_main_content(SessionId, Id) :- dom_attr(SessionId, Id, "role", "main").
is_main_content(SessionId, Id) :-
    dom_attr(SessionId, Id, "class", Class),
    :string:contains(Class, "main-content").

# --- Primary Action Detection ---
Decl primary_action(SessionId, Ref, Label).
primary_action(SessionId, Ref, Label) :-
    interactive(SessionId, Ref, "button", Label, _),
    elem_attr(SessionId, Ref, "button_type", "submit").
primary_action(SessionId, Ref, Label) :-
    interactive(SessionId, Ref, "button", Label, _),
    elem_attr(SessionId, Ref, "data_testid", TestID),
    :string:contains(TestID, "cta").
primary_action(SessionId, Ref, Label) :-
    interactive(SessionId, Ref, "button", Label, _),
    elem_bbox(SessionId, Ref, _, _, W, H),
    W >= 200,
    H >= 40.
primary_action(SessionId, Ref, Label) :-
    interactive(SessionId, Ref, "button", Label, _),
    elem_attr(SessionId, Ref, "id", Id),
    :string:contains(Id, "submit").
