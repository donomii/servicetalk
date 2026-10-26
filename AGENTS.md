# Agent Notes

You are not allowed to change the formatting.

Agent workflow:
- Before anything else, read the SPEC.md
- After making changes based on user preferences, update the SPEC.md to reflect the new behavior.

# SPEC.md

The SPEC file is a very important document that describes, in detail, everything about this project in enough detail that the entire project can be re-created entirely from the spec.  The therefore must include:

- A solid summary of the project
- a detailed description of all user interactions.  Every window, button, click, response, and behaviour must be documented in detail.
- The exact layout is not important.  If the spec says "and the user clicks the OK button to continue", you can put the OK button wherever you want.  The important thing is that the user can continue.
- It must contain a description of all major datatypes used in the program, with detailed annotations on their use.  Major means global datatypes, and datatypes that are used for interacting with other systems (API calls, FFI calls, database calls, etc.

- If the program expects files to be on disk, e.g. picture files or .config files, the spec must describe the expected file format, and where the files are expected to be found, in addition to all the other requirements above (i.e. external data types, etc.)


# git

you are forbidden from using any git commands, at all.  You are double definitely forbidden from using destructive git commands.

# coding style

Read CODING.md and obey the coding style specified.

# pseudocode

Create a directory called pseudocode, and put all pseudocode in there.

Pseudocode is a compact, human-readable description of an algorithm or program.  It concerns itself with the logic of the program, not the implementation details.  Pseudocode details function names, their inputs and outputs, the data types they use, and the logic they perform.  It does not specify the programming language used to implement the algorithm.  It does not specify how to carry out the task, only what needs to be done.  For instance, "remove nulls from input" is a good pseudocode description of a function.  "remove nulls from input by iterating through the input and removing nulls" is not.  Also, do not specify a datatype unless required.  You do not need to say "remove nulls from list" or "remove nulls from array", only "remove nulls".

The exact strategy to implement the task is left to the implementation.

Pseudocode should describe the publically visible functions and datatypes of a module.  It should not describe private, or utility functions.  The things described in pseudocode are expected to be relevant to the entire program, or at least to other modules that use the abilities of the specified module.

Generally short or utility functions are not described in pseudocode, unless they are particularly complex or important.

You should create a pseudocode file for each module, in the pseudocode directory.  The file should be named the same as the module, but with a .pseudocode extension.

Pseudocode files should be updated whenever the module is updated.

The pseudocode should be enough to recreate the entire project from scratch, without reference to the original code.  

- Objects must document their public methods and fields.  Private methods are not documented unless they are particularly complex or important.
- Small, simple helper functions are not documented unless they are particularly complex or important.
- Temporary variables or functions are not documented.  e.g. a variable only used inside a single function is not documented.
- When describing functions, objects, methods, and datatypes, don't describe them in terms of a specific programming language, unless that is vital to the function of the program.  e.g. specify numbers as floats, integers, etc. not as int16, float64, etc.  Strings are just strings, not byte arrays, or UTF-8, etc.
- if an algorithm or data type requires a specific type, like a 8 bit integer, or a 32 bit float, then be sure to specify that, and the reason why it is required.
- Algorithms should be described in terms of pseudocode, not in terms of a specific programming language.


# Readme.md

If a README.md already exists, you will not touch it, or edit it in anyway.  It is strictly forbidden to modify it.

If the readme does not exist, you will create one.  

when you create one it will contain a project introduction, and instructions on how to build and run the project.  

The introduction must be written in a way that is accessible to someone who is not familiar with the project.  It explains what the project does, and why someone would want to use it.  

## Code comments.

You will not remove any comments from the code, at all, for any reason.


## Code Quality Checklist

Before marking work complete:
- [ ] Code is readable and well-named
- [ ] Functions are small (<50 lines)
- [ ] Files are focused (<800 lines)
- [ ] No deep nesting (>4 levels)
- [ ] Proper error handling
- [ ] No hardcoded values
- [ ] No void* or similar types
- [ ] SPEC.md updated
- [ ] Work is not complete until the code builds and tests are run
- [ ] No formatting changes