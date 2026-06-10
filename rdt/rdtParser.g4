parser grammar RdtParser;

options {
	tokenVocab = RdtLexer;
}

entrypoint: expression EOF;

expression: union;

union: type (WS* PIPE WS* type)*;

type: (primitive | group | reference) ARRAY_NOTATION* OPTIONAL_NOTATION?;

primitive:
	STRING_TYPE
	| INTEGER_TYPE
	| NUMBER_TYPE
	| BOOLEAN_TYPE
	| DATETIME_TYPE
	| TIME_ONLY_TYPE
	| DATETIME_ONLY_TYPE
	| DATE_ONLY_TYPE
	| FILE_TYPE
	| NIL_TYPE
	| ANY_TYPE
	| ARRAY_TYPE
	| OBJECT_TYPE
	| UNION_TYPE;

group: LPAREN WS* expression WS* RPAREN;

reference: IDENTIFIER (DOT IDENTIFIER)?;
