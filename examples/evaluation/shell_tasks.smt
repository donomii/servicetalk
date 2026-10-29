"Compose ordinary command results as text. Requires the sort and uniq tools."
shell := Shell new.
sorted := shell run: 'sort' args: #() input: 'tea
coffee
tea
milk
'.
(sorted exitCode = 0) ifFalse: [ (sorted error , sorted stderr) signal ].
report := shell run: 'uniq' args: #('-c') input: sorted stdout.
(report exitCode = 0) ifFalse: [ (report error , report stderr) signal ].
Transcript show: report stdout.
shell jobs
