"A live task list, inherited methods, and an independent object clone."
check := EvaluationChecks new.
first := EvaluationTask new; title: 'Write report'; yourself.
second := EvaluationTimedTask new; title: 'Review report'; minutes: 15; yourself.
tasks := {first. second}.
first complete.
open := tasks select: [:task | task completed not].
check equal: open size to: 1 named: 'open task count'.
check equal: open first summary to: 'Review report (open) — 15 min' named: 'inherited summary'.
copy := second clone.
copy title: 'Review a copy'; complete.
check equal: second completed to: false named: 'clone state independence'.
check equal: copy completed to: true named: 'copy completion'.
check equal: copy minutes to: 15 named: 'inherited clone field'.
Transcript show: 'task objects, inheritance and cloning passed'.
(tasks collect: [:task | task summary]) join: ', '.
